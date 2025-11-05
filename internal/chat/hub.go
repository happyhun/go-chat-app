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
		stop:           make(chan []byte),
	}
}

// GetClientCount returns the current number of clients in the hub.
func (h *Hub) GetClientCount() int {
	respChan := make(chan int)
	h.getClientCount <- respChan
	return <-respChan
}

// Run starts the main event loop for the hub.
// It handles client registration, unregistration, and message broadcasting.
func (h *Hub) Run() {
	log.Printf("INFO: Hub '%s' started.", h.ID)
	defer func() { log.Printf("INFO: Hub '%s' stopped.", h.ID) }()
	for {
		select {
		case reg := <-h.register:
			h.handleRegistration(reg)
		case client := <-h.unregister:
			h.handleUnregistration(client)
		case payload := <-h.broadcast:
			h.handleBroadcast(payload)
		case respChan := <-h.getClientCount:
			respChan <- len(h.clients)
		case shutdownMsg := <-h.stop:
			for client := range h.clients {
				client.sendSafe(shutdownMsg)
				close(client.send)
			}
			return
		}
	}
}

// handleRegistration registers a new client to the hub.
func (h *Hub) handleRegistration(reg *registration) {
	reg.client.Nickname = reg.nickname
	h.clients[reg.client] = true

	successMsg, err := NewMessage(MsgTypeRegisterSuccess, reg.client.ID, "", "Successfully joined the chat room.")
	if err != nil {
		log.Printf("ERROR: failed to create registration success message: %v", err)
	} else {
		reg.client.sendSafe(successMsg)
	}
	log.Printf("INFO: Client %s (nickname: %s) registered to hub '%s'.", reg.client.ID, reg.nickname, h.ID)

	joinMsg, err := NewMessage(MsgTypeUserJoin, reg.client.ID, reg.nickname, "")
	if err != nil {
		log.Printf("ERROR: failed to create user join message: %v", err)
		return
	}
	h.broadcast <- &broadcastPayload{message: joinMsg, client: nil}

	h.broadcastUserCountUpdate()
	h.manager.notifyLobbyUpdate()
}

// handleUnregistration unregisters a client from the hub.
func (h *Hub) handleUnregistration(client *Client) {
	if _, ok := h.clients[client]; !ok {
		return
	}

	delete(h.clients, client)
	close(client.send)

	if client.Nickname != "" {
		log.Printf("INFO: Client %s (nickname: %s) unregistered from hub.", client.ID, client.Nickname)

		leaveMsg, err := NewMessage(MsgTypeUserLeave, client.ID, client.Nickname, "")
		if err != nil {
			log.Printf("ERROR: failed to create user leave message: %v", err)
		} else {
			h.broadcast <- &broadcastPayload{message: leaveMsg, client: nil}
		}
	}

	h.broadcastUserCountUpdate()
	h.manager.notifyLobbyUpdate()

	if len(h.clients) == 0 {
		h.manager.RemoveHub(h.ID)
	}
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
	userCountMsg, err := NewMessage(MsgTypeUserCount, "", "", len(h.clients))
	if err != nil {
		log.Printf("ERROR: failed to create user count update message: %v", err)
		return
	}
	for client := range h.clients {
		client.sendSafe(userCountMsg)
	}
}
