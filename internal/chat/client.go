package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

// ServeWS handles websocket requests from the peer.
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request, appMaxChatMessageLength int, allowedOrigins []string) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			if len(allowedOrigins) == 0 {
				return true
			}
			origin := r.Header.Get("Origin")
			for _, allowed := range allowedOrigins {
				if allowed == origin {
					return true
				}
			}
			hub.logger.Warn("Origin not allowed", "origin", origin)
			return false
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		hub.logger.Error("failed to upgrade connection", "err", err)
		return
	}

	// The nickname is passed via query parameter for WebSocket connections.
	// It's pre-validated and registered by the lobby (SSE handler).
	nickname := r.URL.Query().Get("nickname")
	if nickname == "" {
		// This check is now valid as the frontend is expected to provide the nickname.
		hub.logger.Warn("WebSocket connection attempt without a nickname. Closing.")
		// We can't write an http.Error after the upgrade has started.
		// The connection will just be closed, and the client will have to handle it.
		return
	}

	client := &Client{
		ID:                      uuid.NewString(),
		Nickname:                nickname, // Set nickname on connection
		hub:                     hub,
		conn:                    conn,
		send:                    make(chan []byte, 256),
		appMaxChatMessageLength: appMaxChatMessageLength,
		logger:                  hub.logger.With("clientID", uuid.NewString(), "nickname", nickname),
	}

	// Register the client immediately.
	hub.register <- &registration{client: client, nickname: client.Nickname}

	go client.writePump()
	go client.readPump()
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	ID                      string
	Nickname                string
	hub                     *Hub
	conn                    *websocket.Conn
	send                    chan []byte // Buffered channel of outbound messages.
	appMaxChatMessageLength int
	logger                  *slog.Logger
}

// readPump pumps messages from the websocket connection to the hub.
// It is the single source of truth for the connection's lifecycle.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c

		// CRITICAL: Always release the nickname.
		// This ensures that even if registration fails, the nickname reserved
		// in the lobby is freed, preventing it from being locked forever.
		c.hub.manager.RemoveNickname(c.Nickname)
		c.logger.Info("Cleaned up client resources")

		if err := c.conn.Close(); err != nil {
			// This error is expected if the write pump has already closed the connection.
			if !strings.Contains(err.Error(), "use of closed network connection") {
				c.logger.Error("closing connection", "err", err)
			}
		}
	}()
	c.conn.SetReadLimit(int64(c.appMaxChatMessageLength * 4)) // Max 4 bytes per rune
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		c.logger.Error("readPump SetReadDeadline failed", "err", err)
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Main read loop
	for {
		_, messageData, err := c.conn.ReadMessage()
		if err != nil {
			// Any error from ReadMessage terminates the pump.
			// We only log errors that are not standard websocket close errors,
			// as these are expected during the connection lifecycle.
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.logger.Error("Unhandled error in readPump", "err", err)
			} else {
				// This is a normal closure, log at a lower level.
				c.logger.Info("readPump closing due to normal websocket closure", "err", err)
			}
			break // Exit loop on error, defer will handle cleanup.
		}

		var incomingMsg IncomingMessage
		if err := json.Unmarshal(messageData, &incomingMsg); err != nil {
			c.logger.Error("failed to unmarshal message from client", "err", err)
			continue
		}

		var outgoingMsgBytes []byte
		var newErr error

		switch incomingMsg.Type {
		case MsgTypeChat:
			trimmedContent := strings.TrimSpace(incomingMsg.Content)
			if trimmedContent == "" {
				continue
			}
			if utf8.RuneCountInString(trimmedContent) > c.appMaxChatMessageLength {
				errorMsg, _ := NewMessage(MsgTypeChatError, c.ID, c.Nickname, fmt.Sprintf("Message is too long (max %d characters).", c.appMaxChatMessageLength))
				c.sendSafe(errorMsg)
				continue
			}
			outgoingMsgBytes, newErr = NewMessage(MsgTypeChat, c.ID, c.Nickname, trimmedContent)
		case MsgTypeTypingStart, MsgTypeTypingStop:
			outgoingMsgBytes, newErr = NewMessage(incomingMsg.Type, c.ID, c.Nickname, "")
		case MsgTypeLeaveRoom:
			c.logger.Info("client requested to leave the room. Closing connection.")
			return // This will trigger the defer and close the connection.
		default:
			c.logger.Warn("unknown message type", "type", incomingMsg.Type)
			continue
		}

		if newErr != nil {
			c.logger.Error("failed to create new message for client", "err", newErr)
			continue
		}
		c.hub.broadcast <- &broadcastPayload{message: outgoingMsgBytes, client: c}
	}
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.logger.Error("writePump SetWriteDeadline failed", "err", err)
				return
			}
			if !ok {
				// The hub closed the channel. The readPump is responsible for closing the connection.
				// The writePump's job is done.
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				c.logger.Error("writePump WriteMessage failed", "err", err)
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				c.logger.Error("writePump ping SetWriteDeadline failed", "err", err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Error("writePump ping WriteMessage failed", "err", err)
				return
			}
		}
	}
}

// sendSafe safely sends a message to the client's send channel.
func (c *Client) sendSafe(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.logger.Warn("client send channel is full. Message dropped.")
	}
}
