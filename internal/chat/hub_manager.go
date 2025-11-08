package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	MaxNicknameLength = 15
	MinNicknameLength = 2
	MaxRoomIDLength   = 20
)

var (
	validNicknameRegex = regexp.MustCompile(`^[a-zA-Z0-9가-힣_]+$`)
	validRoomIDRegex   = regexp.MustCompile(`^[a-zA-Z0-9가-힣_-]+$`)
)

// HubManager manages all chat rooms (Hubs) and global nickname uniqueness.
type HubManager struct {
	rooms          map[string]*Hub
	roomsMu        sync.RWMutex
	nicknames      map[string]bool
	nicknamesMu    sync.RWMutex
	lobbyUpdates   chan bool
	lobbyClients   map[chan string]bool
	lobbyClientsMu sync.RWMutex
	hubWg          sync.WaitGroup // WaitGroup to track all running Hub goroutines.
}

// RoomInfo contains basic information about a chat room for lobby display.
type RoomInfo struct {
	ID    string `json:"id"`
	Users int    `json:"users"`
}

// NewHubManager creates and initializes a new HubManager instance.
func NewHubManager() *HubManager {
	m := &HubManager{
		rooms:        make(map[string]*Hub),
		nicknames:    make(map[string]bool),
		lobbyUpdates: make(chan bool, 1),
		lobbyClients: make(map[chan string]bool),
	}
	go m.lobbyBroadcastRoutine()
	return m
}

// lobbyBroadcastRoutine listens for lobby update notifications and broadcasts
// the current room list to all registered lobby clients.
func (m *HubManager) lobbyBroadcastRoutine() {
	for range m.lobbyUpdates {
		rooms := m.ListRooms()
		jsonData, err := json.Marshal(rooms)
		if err != nil {
			log.Printf("ERROR: failed to marshal lobby data: %v", err)
			continue
		}
		m.lobbyClientsMu.RLock()
		for clientChan := range m.lobbyClients {
			select {
			case clientChan <- string(jsonData):
			default:
				log.Printf("WARN: lobby client channel full, skipping message.")
			}
		}
		m.lobbyClientsMu.RUnlock()
	}
}

// notifyLobbyUpdate sends a notification to update the lobby.
func (m *HubManager) notifyLobbyUpdate() {
	select {
	case m.lobbyUpdates <- true:
	default:
	}
}

// RegisterLobbyClient registers a new client channel to receive lobby updates.
func (m *HubManager) RegisterLobbyClient(clientChan chan string) {
	m.lobbyClientsMu.Lock()
	m.lobbyClients[clientChan] = true
	m.lobbyClientsMu.Unlock()
}

// UnregisterLobbyClient unregisters a client channel from receiving lobby updates.
func (m *HubManager) UnregisterLobbyClient(clientChan chan string) {
	m.lobbyClientsMu.Lock()
	defer m.lobbyClientsMu.Unlock()
	delete(m.lobbyClients, clientChan)
}

// RemoveHub removes a hub from the HubManager.
func (m *HubManager) RemoveHub(hubID string) {
	m.roomsMu.Lock()
	defer m.roomsMu.Unlock()

	if _, ok := m.rooms[hubID]; ok {
		delete(m.rooms, hubID)
		log.Printf("INFO: Hub '%s' removed from manager as it became empty.", hubID)
		m.notifyLobbyUpdate()
	}
}

// ShutdownAllHubs sends a shutdown message to all active hubs and waits for them to finish.
func (m *HubManager) ShutdownAllHubs() {
	m.roomsMu.RLock()
	hubsToShutdown := make([]*Hub, 0, len(m.rooms))
	for _, hub := range m.rooms {
		hubsToShutdown = append(hubsToShutdown, hub)
	}
	m.roomsMu.RUnlock()

	shutdownMsg, _ := NewMessage(MsgTypeServerShutdown, "", "", "Server is shutting down for maintenance. Please try again later.")

	for _, hub := range hubsToShutdown {
		hub.stop <- shutdownMsg
	}

	// Wait for all Hub goroutines to completely finish.
	m.hubWg.Wait()

	// Now that all hubs are stopped, it's safe to close the lobbyUpdates channel.
	close(m.lobbyUpdates)
	log.Println("INFO: All hubs have been shut down.")
}

// IsNicknameAvailable checks if a nickname is available and valid.
func (m *HubManager) IsNicknameAvailable(nickname string) (bool, error) {
	validatedNickname, err := validateNickname(nickname)
	if err != nil {
		return false, err
	}
	m.nicknamesMu.RLock()
	defer m.nicknamesMu.RUnlock()
	return !m.nicknames[validatedNickname], nil
}

// AddNickname validates and adds a nickname to the global list.
func (m *HubManager) AddNickname(nickname string) (string, error) {
	validatedNickname, err := validateNickname(nickname)
	if err != nil {
		return "", err
	}
	m.nicknamesMu.Lock()
	defer m.nicknamesMu.Unlock()
	if m.nicknames[validatedNickname] {
		return "", errors.New("nickname is already in use")
	}
	m.nicknames[validatedNickname] = true
	return validatedNickname, nil
}

// RemoveNickname removes a nickname from the global list.
func (m *HubManager) RemoveNickname(nickname string) {
	m.nicknamesMu.Lock()
	defer m.nicknamesMu.Unlock()
	delete(m.nicknames, nickname)
}

// validateNickname checks the validity of a nickname (length, allowed characters).
func validateNickname(nickname string) (string, error) {
	trimmed := strings.TrimSpace(nickname)
	charCount := utf8.RuneCountInString(trimmed)
	if charCount < MinNicknameLength || charCount > MaxNicknameLength {
		return "", fmt.Errorf("nickname must be between %d and %d characters", MinNicknameLength, MaxNicknameLength)
	}
	if !validNicknameRegex.MatchString(trimmed) {
		return "", errors.New("nickname can only contain letters, numbers, and underscores")
	}
	return trimmed, nil
}

// validateRoomID checks the validity of a room ID (length, allowed characters).
func validateRoomID(roomID string) (string, error) {
	trimmed := strings.TrimSpace(roomID)
	charCount := utf8.RuneCountInString(trimmed)
	if charCount == 0 {
		return "", errors.New("room name cannot be empty")
	}
	if charCount > MaxRoomIDLength {
		return "", fmt.Errorf("room name can be at most %d characters long", MaxRoomIDLength)
	}
	if !validRoomIDRegex.MatchString(trimmed) {
		return "", errors.New("room name can only contain letters, numbers, hyphens, and underscores")
	}
	return trimmed, nil
}

// CreateHub creates a new hub, starts it, and adds it to the manager.
func (m *HubManager) CreateHub(roomID string) (*Hub, error) {
	validatedRoomID, err := validateRoomID(roomID)
	if err != nil {
		return nil, err
	}
	m.roomsMu.Lock()
	defer m.roomsMu.Unlock()

	if _, ok := m.rooms[validatedRoomID]; ok {
		return nil, errors.New("a room with this name already exists")
	}

	hub := NewHub(validatedRoomID, m)
	m.hubWg.Add(1)
	go func() {
		defer m.hubWg.Done()
		hub.Run()
	}()
	m.rooms[validatedRoomID] = hub
	m.notifyLobbyUpdate()
	log.Printf("INFO: Hub '%s' created.", validatedRoomID)
	return hub, nil
}

// GetHub returns a hub by its ID.
func (m *HubManager) GetHub(roomID string) (*Hub, error) {
	validatedRoomID, err := validateRoomID(roomID)
	if err != nil {
		return nil, err
	}
	m.roomsMu.RLock()
	defer m.roomsMu.RUnlock()

	if hub, ok := m.rooms[validatedRoomID]; ok {
		return hub, nil
	}

	return nil, errors.New("room does not exist or has been deleted")
}

// ListRooms returns a list of all active chat rooms with their user counts.
func (m *HubManager) ListRooms() []RoomInfo {
	m.roomsMu.RLock()
	defer m.roomsMu.RUnlock()
	roomList := make([]RoomInfo, 0, len(m.rooms))
	for id, hub := range m.rooms {
		roomList = append(roomList, RoomInfo{ID: id, Users: hub.GetClientCount()})
	}
	return roomList
}
