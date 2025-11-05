package server

import (
	"encoding/json"
	"fmt"
	"go-chat-app/internal/chat"
	"log"
	"net/http"
	"strings"
)

// registerRoutes registers all HTTP routes for the server.
func (s *Server) registerRoutes() {
	// Serve static files
	s.router.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	// Serve the main application page
	s.router.HandleFunc("/", s.serveHome())
	// Handle favicon requests
	s.router.HandleFunc("/favicon.ico", s.handleFavicon())

	// API routes
	api := http.NewServeMux()
	api.HandleFunc("/config", s.configHandler())                 // GET client configuration
	api.HandleFunc("/nicknames/check", s.nicknameCheckHandler()) // POST to check nickname availability
	api.HandleFunc("/rooms", s.roomsHandler())                   // GET room list, POST to create a room
	api.HandleFunc("/rooms/stream", s.sseHandler())              // GET stream of lobby updates
	s.router.Handle("/api/", http.StripPrefix("/api", api))

	// WebSocket handler
	s.router.HandleFunc("/ws/", s.wsHandler())
}

// cspMiddleware sets the Content Security Policy header.
func (s *Server) cspMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// serveHome serves the index.html file.
func (s *Server) serveHome() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "web/templates/index.html")
	}
}

// handleFavicon handles favicon requests with a 204 No Content response.
func (s *Server) handleFavicon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
}

// nicknameCheckHandler handles requests to check if a nickname is available.
// It expects a POST request with a JSON body containing the nickname.
func (s *Server) nicknameCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		var reqBody struct {
			Nickname string `json:"nickname"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		available, err := s.hubManager.IsNicknameAvailable(reqBody.Nickname)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]bool{"available": available}); err != nil {
			log.Printf("ERROR: failed to encode nickname availability response: %v", err)
		}
	}
}

// roomsHandler handles requests related to chat rooms.
// It routes GET requests to listRoomsHandler and POST requests to createRoomHandler.
func (s *Server) roomsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.listRoomsHandler(w, r)
		case http.MethodPost:
			s.createRoomHandler(w, r)
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}
}

// listRoomsHandler returns a JSON list of currently active chat rooms.
func (s *Server) listRoomsHandler(w http.ResponseWriter, _ *http.Request) {
	rooms := s.hubManager.ListRooms()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rooms); err != nil {
		log.Printf("ERROR: failed to encode room list response: %v", err)
	}
}

// createRoomHandler creates a new chat room.
// It expects a POST request with a JSON body containing the roomID.
func (s *Server) createRoomHandler(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		RoomID string `json:"roomID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	_, err := s.hubManager.CreateHub(reqBody.RoomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "created", "roomID": reqBody.RoomID}); err != nil {
		log.Printf("ERROR: failed to encode create room response: %v", err)
	}
}

// configHandler provides client configuration, such as message length limits.
func (s *Server) configHandler() http.HandlerFunc {
	type clientConfig struct {
		MaxChatMessageLength int `json:"maxChatMessageLength"`
		MaxNicknameLength    int `json:"maxNicknameLength"`
		MinNicknameLength    int `json:"minNicknameLength"`
		MaxRoomIDLength      int `json:"maxRoomIDLength"`
	}
	cfg := clientConfig{
		MaxChatMessageLength: s.config.MaxChatMessageLength,
		MaxNicknameLength:    chat.MaxNicknameLength,
		MinNicknameLength:    chat.MinNicknameLength,
		MaxRoomIDLength:      chat.MaxRoomIDLength,
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cfg); err != nil {
			log.Printf("ERROR: failed to encode config response: %v", err)
		}
	}
}

// sseHandler handles Server-Sent Events (SSE) for lobby updates.
// It registers a client for lobby updates and streams changes to the room list.
func (s *Server) sseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nickname := r.URL.Query().Get("nickname")
		if nickname == "" {
			http.Error(w, "Nickname is required for lobby stream", http.StatusBadRequest)
			return
		}

		validatedNickname, err := s.hubManager.AddNickname(nickname)
		if err != nil {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if _, err := fmt.Fprintf(w, "data: error: %s\n\n", err); err != nil {
				log.Printf("ERROR: failed to send SSE error message: %v", err)
			}
			w.(http.Flusher).Flush()
			log.Printf("INFO: SSE nickname registration failed for %s: %v", nickname, err)
			return
		}

		defer func() {
			s.hubManager.RemoveNickname(validatedNickname)
			log.Printf("INFO: SSE client disconnected, nickname '%s' released", validatedNickname)
		}()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		clientChan := make(chan string, 2)
		s.hubManager.RegisterLobbyClient(clientChan)
		defer s.hubManager.UnregisterLobbyClient(clientChan)

		log.Printf("INFO: SSE client connected (nickname: %s)", validatedNickname)

		initialRooms := s.hubManager.ListRooms()
		jsonData, err := json.Marshal(initialRooms)
		if err != nil {
			log.Printf("ERROR: failed to marshal initial lobby data: %v", err)
		} else {
			if _, err := fmt.Fprintf(w, "data: %s\n\n", jsonData); err != nil {
				log.Printf("ERROR: failed to send initial SSE room list: %v", err)
			}
		}
		w.(http.Flusher).Flush()

		for {
			select {
			case eventData := <-clientChan:
				if _, err := fmt.Fprintf(w, "data: %s\n\n", eventData); err != nil {
					log.Printf("ERROR: failed to send SSE event data: %v", err)
				}
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}

// wsHandler handles WebSocket connection requests.
// It upgrades the HTTP connection to a WebSocket and connects the client to the appropriate hub.
func (s *Server) wsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roomID := strings.TrimPrefix(r.URL.Path, "/ws/")

		hub, err := s.hubManager.GetHub(roomID)
		if err != nil {
			log.Printf("ERROR: failed to get hub for room '%s': %v", roomID, err)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		chat.ServeWS(hub, w, r, s.config.MaxChatMessageLength, s.config.AllowedOrigins)
	}
}
