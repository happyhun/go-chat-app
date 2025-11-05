package chat

import (
	"encoding/json"
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
			// If ALLOWED_ORIGINS is not set, allow all origins for development convenience (e.g., for ngrok).
			// A warning is logged on startup in main.go.
			if len(allowedOrigins) == 0 {
				return true
			}

			// Otherwise, check if the request origin is in the list of allowed origins.
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
		// The upgrader logs the error, so we don't have to.
		return
	}

	client := &Client{
		ID:                      uuid.NewString(),
		Nickname:                "", // Nickname will be set by the first REGISTER message
		hub:                     hub,
		conn:                    conn,
		send:                    make(chan []byte, 256),
		appMaxChatMessageLength: appMaxChatMessageLength,
		serverInitiatedClose:    false,
		quit:                    make(chan struct{}),
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
	send                    chan []byte
	appMaxChatMessageLength int
	serverInitiatedClose    bool
	quit                    chan struct{}
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application ensures that there is at most one reader on a connection by
// executing all reads from this goroutine. It is responsible for closing the
// connection and signaling the writePump to exit.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		close(c.quit) // Signal writePump to exit
		// The connection is closed here. We log only unexpected errors.
		if err := c.conn.Close(); err != nil && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			log.Printf("ERROR: unexpected error closing connection for client %s: %v", c.ID, err)
		}
	}()

	maxFrameSize := int64(c.appMaxChatMessageLength * 4) // Assume max 4 bytes per rune
	c.conn.SetReadLimit(maxFrameSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Printf("ERROR: failed to set initial read deadline for client %s: %v", c.ID, err)
	}
	c.conn.SetPongHandler(func(string) error {
		if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			log.Printf("ERROR: failed to set read deadline on pong for client %s: %v", c.ID, err)
		}
		return nil
	})

	// The first message must be a registration message.
	_, messageData, err := c.conn.ReadMessage()
	if err != nil {
		if !c.serverInitiatedClose {
			log.Printf("ERROR: failed to read initial message from client %s: %v", c.ID, err)
		}
		return
	}

	var regMsg IncomingMessage
	if err := json.Unmarshal(messageData, &regMsg); err != nil || regMsg.Type != MsgTypeRegister {
		log.Printf("ERROR: invalid registration message from client %s: %v", c.ID, err)
		errorMsg, msgErr := NewMessage(MsgTypeRegisterError, "", "", "Invalid registration message. Connection closed.")
		if msgErr != nil {
			log.Printf("ERROR: failed to create registration error message: %v", msgErr)
		} else {
			c.sendSafe(errorMsg)
		}
		return
	}

	c.hub.register <- &registration{client: c, nickname: regMsg.Content}

	for {
		_, messageData, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ERROR: unexpected close error from client %s: %v", c.ID, err)
			}
			break
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
				log.Printf("WARN: client %s sent message exceeding max length", c.ID)
				errorMsg, err := NewMessage(MsgTypeChatError, c.ID, c.Nickname, fmt.Sprintf("Message is too long (max %d characters).", c.appMaxChatMessageLength))
				if err != nil {
					log.Printf("ERROR: failed to create chat error message: %v", err)
				} else {
					c.sendSafe(errorMsg)
				}
				continue
			}
			outgoingMsgBytes, newErr = NewMessage(MsgTypeChat, c.ID, c.Nickname, trimmedContent)

		case MsgTypeTypingStart, MsgTypeTypingStop:
			outgoingMsgBytes, newErr = NewMessage(incomingMsg.Type, c.ID, c.Nickname, "")

		case MsgTypeLeaveRoom:
			log.Printf("INFO: client %s requested to leave the room. Closing connection.", c.ID)
			c.serverInitiatedClose = true
			return

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
//
// A single goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer on a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("ERROR: failed to set write deadline for client %s: %v", c.ID, err)
				return
			}
			if !ok {
				// The hub closed the channel.
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("ERROR: failed to write message to client %s: %v", c.ID, err)
				return
			}
		case <-ticker.C:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				log.Printf("ERROR: failed to set write deadline for ping on client %s: %v", c.ID, err)
				return
			}
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("ERROR: failed to send ping to client %s: %v", c.ID, err)
				return
			}
		case <-c.quit:
			log.Printf("INFO: writePump for client %s received quit signal. Exiting.", c.ID)
			return
		}
	}
}

// sendSafe safely sends a message to the client's send channel.
// If the channel is full, it unregisters the client.
func (c *Client) sendSafe(msg []byte) {
	select {
	case c.send <- msg:
	default:
		log.Printf("WARN: client %s send channel is full. Unregistering client.", c.ID)
		c.hub.unregister <- c
	}
}
