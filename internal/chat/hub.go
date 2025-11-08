package chat

import (
	"log"
)

// Hub manages the clients and message broadcasting for a single chat room.
type Hub struct {
	ID             string
	manager        *HubManager
	clients        map[*Client]bool       // Registered clients.
	broadcast      chan *broadcastPayload // Inbound messages from the clients.
	register       chan *registration     // Register requests from clients.
	unregister     chan *Client           // Unregister requests from clients.
	getClientCount chan chan int          // Channel to request the current client count.
	stop           chan []byte            // Channel to signal hub shutdown.
}

// broadcastPayload contains the message and the sender client.
type broadcastPayload struct {
	message []byte
	client  *Client // The client who sent the message. If nil, broadcast to all.
}

// registration contains the client and nickname for a new registration.
type registration struct {
	client   *Client
	nickname string
}

// NewHub creates and initializes a new Hub instance.
func NewHub(id string, manager *HubManager) *Hub {
	return &Hub{
		ID:             id,
		manager:        manager,
		clients:        make(map[*Client]bool),
		broadcast:      make(chan *broadcastPayload, 256),
		register:       make(chan *registration),
		unregister:     make(chan *Client),
		getClientCount: make(chan chan int),
		stop:           make(chan []byte, 1), // Buffered to prevent blocking on send.
	}
}

// GetClientCount returns the current number of clients in the hub.
func (h *Hub) GetClientCount() int {
	respChan := make(chan int)
	select {
	case h.getClientCount <- respChan:
		return <-respChan
	case <-h.stop: // If hub is stopping, don't block and return 0.
		return 0
	}
}

// Run starts the main event loop for the hub.
func (h *Hub) Run() {
	log.Printf("INFO: Hub '%s' started.", h.ID)
	defer func() { log.Printf("INFO: Hub '%s' stopped.", h.ID) }()

	for {
		select {
		case reg := <-h.register:
			h.handleRegistration(reg)

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				h.handleUnregistration(client)
				// If the hub became empty naturally (not during a server shutdown),
				// remove it from the manager and stop its own goroutine.
				if len(h.clients) == 0 {
					h.manager.RemoveHub(h.ID)
					return // Stop the hub's goroutine.
				}
			}

		case payload := <-h.broadcast:
			h.handleBroadcast(payload)

		case respChan := <-h.getClientCount:
			respChan <- len(h.clients)

		case shutdownMsg := <-h.stop:
			close(h.stop) // Mark that we are shutting down.

			// Notify all clients about the shutdown and close their send channels.
			for client := range h.clients {
				client.sendSafe(shutdownMsg)
				close(client.send)
			}

			// Wait for all clients to unregister.
			for len(h.clients) > 0 {
				client := <-h.unregister
				h.handleUnregistration(client)
			}
			return // All clients have unregistered, exit the Run method.
		}
	}
}

// handleRegistration registers a new client to the hub.
func (h *Hub) handleRegistration(reg *registration) {
	h.clients[reg.client] = true
	reg.client.Nickname = reg.nickname

	successMsg, _ := NewMessage(MsgTypeRegisterSuccess, reg.client.ID, "", "Successfully joined the chat room.")
	reg.client.sendSafe(successMsg)

	log.Printf("INFO: Client %s (nickname: %s) registered to hub '%s'.", reg.client.ID, reg.nickname, h.ID)

	joinMsg, _ := NewMessage(MsgTypeUserJoin, reg.client.ID, reg.nickname, "")
	h.broadcast <- &broadcastPayload{message: joinMsg, client: nil}

	h.broadcastUserCountUpdate()
	h.manager.notifyLobbyUpdate()
}

// handleUnregistration unregisters a client from the hub.
func (h *Hub) handleUnregistration(client *Client) {
	// The check for existence is now done in the caller.
	delete(h.clients, client)

	if client.Nickname != "" {
		log.Printf("INFO: Client %s (nickname: %s) unregistered from hub.", client.ID, client.Nickname)
		leaveMsg, _ := NewMessage(MsgTypeUserLeave, client.ID, client.Nickname, "")
		// Use a non-blocking broadcast to avoid deadlocks if the hub is also shutting down.
		select {
		case h.broadcast <- &broadcastPayload{message: leaveMsg, client: nil}:
		default:
		}
	}

	h.broadcastUserCountUpdate()
	h.manager.notifyLobbyUpdate()
}

// handleBroadcast broadcasts a message to all clients in the hub.
func (h *Hub) handleBroadcast(payload *broadcastPayload) {
	for client := range h.clients {
		if payload.client == nil || client != payload.client {
			client.sendSafe(payload.message)
		}
	}
}

// broadcastUserCountUpdate broadcasts the current user count to all clients in the hub.
func (h *Hub) broadcastUserCountUpdate() {
	userCountMsg, _ := NewMessage(MsgTypeUserCount, "", "", len(h.clients))
	// Use a non-blocking broadcast to avoid deadlocks if the hub is also shutting down.
	select {
	case h.broadcast <- &broadcastPayload{message: userCountMsg, client: nil}:
	default:
	}
}
