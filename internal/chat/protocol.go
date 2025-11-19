package chat

import (
	"encoding/json"
	"time"
)

// WebSocket message types
const (
	// MsgTypeChat is for general chat messages from a client.
	MsgTypeChat = "chat"
	// MsgTypeUserJoin is a server-to-client message announcing a user has joined.
	MsgTypeUserJoin = "user_join"
	// MsgTypeUserLeave is a server-to-client message announcing a user has left.
	MsgTypeUserLeave = "user_leave"
	// MsgTypeUserCount is a server-to-client message for updating the user count.
	MsgTypeUserCount = "user_count"
	// MsgTypeWelcome is a server-to-client message sent upon successful connection.
	MsgTypeWelcome = "welcome"
	// MsgTypeChatError is a server-to-client message for chat-related errors (e.g., message too long).
	MsgTypeChatError = "chat_error"
	// MsgTypeTypingStart is a client-to-server message indicating the user started typing.
	MsgTypeTypingStart = "typing_start"
	// MsgTypeTypingStop is a client-to-server message indicating the user stopped typing.
	MsgTypeTypingStop = "typing_stop"
	// MsgTypeServerShutdown is a server-to-client message announcing a server shutdown.
	MsgTypeServerShutdown = "server_shutdown"
	// MsgTypeLeaveRoom is a client-to-server message indicating the user wants to leave the room.
	MsgTypeLeaveRoom = "leave_room"
)

// Message is the standard message structure exchanged between server and client.
type Message struct {
	Type      string      `json:"type"`
	ID        string      `json:"id,omitempty"`
	Nickname  string      `json:"nickname,omitempty"`
	Content   interface{} `json:"content"`
	Timestamp string      `json:"timestamp"`
}

// IncomingMessage is a structure for parsing messages from the client.
// Client messages are expected to only contain Type and Content.
type IncomingMessage struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// NewMessage creates a new Message object and serializes it to a JSON byte slice.
// It populates the message with the given type, ID, nickname, content, and the current timestamp.
func NewMessage(msgType, id, nickname string, content interface{}) ([]byte, error) {
	msg := &Message{
		Type:      msgType,
		ID:        id,
		Nickname:  nickname,
		Content:   content,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	return json.Marshal(msg)
}
