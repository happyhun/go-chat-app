package chat

import (
	"log"
	"sync"
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
	stop           chan struct{}          // Channel to signal hub shutdown.
	stopOnce       sync.Once              // Ensures the stop logic is executed only once.
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
		stop:           make(chan struct{}),
	}
}

// GetClientCount returns the current number of clients in the hub.
func (h *Hub) GetClientCount() int {
	respChan := make(chan int)
	select {
	case h.getClientCount <- respChan:
		return <-respChan
	case <-h.stop:
		// If the hub is stopped or stopping, return 0 immediately.
		return 0
	default:
		// If the hub's Run loop is busy, it might not be able to respond.
		// Returning 0 is a safe fallback to prevent blocking the caller.
		return 0 // Avoid blocking
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
				hubBecameEmpty := h.handleUnregistration(client)
				// If the hub became empty naturally (not during a server shutdown),
				// remove it from the manager and stop its own goroutine.
				if hubBecameEmpty {
					h.manager.removeHub(h.ID)
					return // Stop the hub's goroutine.
				}
			}

		case payload := <-h.broadcast:
			h.handleBroadcast(payload)

		case respChan := <-h.getClientCount:
			respChan <- len(h.clients)

		case <-h.stop:
			// While shutting down, still respond to client count requests to provide accurate numbers.
			// But close the response channel immediately after sending to signal that the hub is gone.
			go func() {
				for respChan := range h.getClientCount {
					respChan <- len(h.clients)
				}
			}()

			// Close all client send channels to signal their writePumps to terminate.
			for client := range h.clients {
				close(client.send)
			}

			// Wait for all clients to unregister.
			// This loop waits for each client's readPump to terminate, which in turn
			// sends a value to the unregister channel.
			for len(h.clients) > 0 {
				client := <-h.unregister
				// During shutdown, we just need to process the unregistration
				// without checking if the hub becomes empty.
				delete(h.clients, client)
				log.Printf("INFO: Client %s (nickname: %s) unregistered during hub shutdown.", client.ID, client.Nickname)
			}

			// Close the getClientCount channel to terminate the temporary goroutine.
			close(h.getClientCount)
			return // All clients have unregistered, exit the Run method.
		}
	}
}

// Stop safely shuts down the hub by closing the stop channel.
// It uses sync.Once to ensure the shutdown logic is executed only once.
func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		// Closing the channel is sufficient to signal all listeners.
		// The Run loop will handle the rest of the cleanup.
		close(h.stop)
	})
}

// handleRegistration registers a new client to the hub.
func (h *Hub) handleRegistration(reg *registration) {
	h.clients[reg.client] = true
	reg.client.Nickname = reg.nickname

	if err := h.sendNewMessage(reg.client, MsgTypeRegisterSuccess, reg.client.ID, "", "Successfully joined the chat room."); err != nil {
		return // Error is logged in sendNewMessage
	}

	log.Printf("INFO: Client %s (nickname: %s) registered to hub '%s'.", reg.client.ID, reg.nickname, h.ID)

	// Broadcast join message to all clients
	h.broadcastNewMessage(nil, MsgTypeUserJoin, reg.client.ID, reg.nickname, "")
	h.broadcastUserCountUpdate()
	h.manager.notifyLobbyUpdate()
}

// handleUnregistration unregisters a client from the hub.
// It returns true if the hub becomes empty after this operation.
func (h *Hub) handleUnregistration(client *Client) (hubBecameEmpty bool) {
	delete(h.clients, client)

	if client.Nickname != "" {
		log.Printf("INFO: Client %s (nickname: %s) unregistered from hub.", client.ID, client.Nickname)
		// Broadcast leave message to all clients
		h.broadcastNewMessage(nil, MsgTypeUserLeave, client.ID, client.Nickname, "")
	}

	h.broadcastUserCountUpdate()
	h.manager.notifyLobbyUpdate()
	return len(h.clients) == 0
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
	h.broadcastNewMessage(nil, MsgTypeUserCount, "", "", len(h.clients))
}

// broadcastNewMessage creates a new message and broadcasts it to clients.
// If the payload's client is nil, it broadcasts to all. Otherwise, it broadcasts to all except the sender.
func (h *Hub) broadcastNewMessage(sender *Client, msgType, id, nickname string, content interface{}) {
	msgBytes, err := NewMessage(msgType, id, nickname, content)
	if err != nil {
		log.Printf("ERROR: Failed to create message for broadcast (type: %s): %v", msgType, err)
		return
	}
	// Use a non-blocking broadcast to avoid deadlocks if the hub is also shutting down.
	select {
	case h.broadcast <- &broadcastPayload{message: msgBytes, client: sender}:
	default:
		log.Printf("WARN: Hub '%s' broadcast channel is full. Message of type '%s' dropped.", h.ID, msgType)
	}
}

// sendNewMessage creates and sends a message to a single client.
func (h *Hub) sendNewMessage(client *Client, msgType, id, nickname string, content interface{}) error {
	msgBytes, err := NewMessage(msgType, id, nickname, content)
	if err != nil {
		log.Printf("ERROR: Failed to create message for client %s (type: %s): %v", client.ID, msgType, err)
		return err
	}
	client.sendSafe(msgBytes)
	return nil
}
