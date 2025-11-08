package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
			log.Printf("WARN: Origin %s not allowed", origin)
			return false
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ERROR: failed to upgrade connection: %v", err)
		return
	}

	client := &Client{
		ID:                      uuid.NewString(),
		Nickname:                "", // Will be set by the first REGISTER message
		hub:                     hub,
		conn:                    conn,
		send:                    make(chan []byte, 256),
		appMaxChatMessageLength: appMaxChatMessageLength,
	}

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
}

// readPump pumps messages from the websocket connection to the hub.
// It is the single source of truth for the connection's lifecycle.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		if err := c.conn.Close(); err != nil {
			// This error is expected if the write pump has already closed the connection.
			if !strings.Contains(err.Error(), "use of closed network connection") {
				log.Printf("ERROR: closing connection for client %s: %v", c.ID, err)
			}
		}
	}()
	c.conn.SetReadLimit(int64(c.appMaxChatMessageLength * 4)) // Max 4 bytes per rune
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Printf("ERROR: readPump SetReadDeadline failed for client %s: %v", c.ID, err)
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// The first message must be a registration message.
	_, messageData, err := c.conn.ReadMessage()
	if err != nil {
		var closeErr *websocket.CloseError
		if !errors.As(err, &closeErr) {
			log.Printf("ERROR: readPump initial read for client %s: %v", c.ID, err)
		}
		return
	}

	var regMsg IncomingMessage
	if err := json.Unmarshal(messageData, &regMsg); err != nil || regMsg.Type != MsgTypeRegister {
		errorMsg, _ := NewMessage(MsgTypeRegisterError, "", "", "Invalid registration message. Connection closed.")
		c.sendSafe(errorMsg)
		log.Printf("ERROR: invalid registration message from client %s: %v", c.ID, err)
		return
	}

	c.hub.register <- &registration{client: c, nickname: regMsg.Content}

	// Main read loop
	for {
		_, messageData, err := c.conn.ReadMessage()
		if err != nil {
			// Any error from ReadMessage terminates the pump.
			// We only log errors that are not standard websocket close errors,
			// as these are expected during the connection lifecycle.
			var closeErr *websocket.CloseError
			if !errors.As(err, &closeErr) {
				log.Printf("ERROR: Unhandled error in readPump for client %s: %v", c.ID, err)
			}
			break // Exit loop on error, defer will handle cleanup.
		}

		var incomingMsg IncomingMessage
		if err := json.Unmarshal(messageData, &incomingMsg); err != nil {
			log.Printf("ERROR: failed to unmarshal message from client %s: %v", c.ID, err)
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
			log.Printf("INFO: client %s requested to leave the room. Closing connection.", c.ID)
			return // This will trigger the defer and close the connection.
		default:
			log.Printf("WARN: unknown message type '%s' from client %s", incomingMsg.Type, c.ID)
			continue
		}

		if newErr != nil {
			log.Printf("ERROR: failed to create new message for client %s: %v", c.ID, newErr)
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
				log.Printf("ERROR: writePump SetWriteDeadline failed for client %s: %v", c.ID, err)
				return
			}
			if !ok {
				// The hub closed the channel. Send a close message to the peer.
				if err := c.conn.WriteMessage(websocket.CloseMessage, []byte{}); err != nil {
					log.Printf("ERROR: writePump writing close message failed for client %s: %v", c.ID, err)
				}
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("ERROR: writePump WriteMessage failed for client %s: %v", c.ID, err)
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("ERROR: writePump ping SetWriteDeadline failed for client %s: %v", c.ID, err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("ERROR: writePump ping WriteMessage failed for client %s: %v", c.ID, err)
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
		log.Printf("WARN: client %s send channel is full. Message dropped.", c.ID)
	}
}
