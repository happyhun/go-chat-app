package server

import (
	"encoding/json"
	"fmt"
	"go-chat-app/internal/chat"
	"log/slog"
	"net/http"
	"strings"
)

// registerRoutes registers all HTTP routes for the server.
func (s *Server) registerRoutes() {
	// Serve static files
	s.router.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	// Serve the main application page
	s.router.HandleFunc("/", s.serveIndexPage())
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

// serveIndexPage serves the main index.html file.
func (s *Server) serveIndexPage() http.HandlerFunc {
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

		w.Header().Set("Content-Type", "application/json")
		available, err := s.hubManager.IsNicknameAvailable(reqBody.Nickname)
		if err != nil {
			// Although IsNicknameAvailable currently doesn't return an error, handling it is good practice.
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else if err := json.NewEncoder(w).Encode(map[string]bool{"available": available}); err != nil {
			s.logger.Error("failed to encode nickname availability response", "err", err)
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
		s.logger.Error("failed to encode room list response", "err", err)
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
	// Respond with the ID of the created room.
	if err := json.NewEncoder(w).Encode(map[string]string{"roomID": reqBody.RoomID}); err != nil {
		s.logger.Error("failed to encode create room response", "err", err)
	}
}

// configHandler provides client configuration, such as message length limits.
func (s *Server) configHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// This struct defines the configuration data exposed to the client.
		clientConfig := struct {
			MaxChatMessageLength int `json:"maxChatMessageLength"`
			MaxNicknameLength    int `json:"maxNicknameLength"`
			MinNicknameLength    int `json:"minNicknameLength"`
			MaxRoomIDLength      int `json:"maxRoomIDLength"`
		}{
			MaxChatMessageLength: s.config.MaxChatMessageLength,
			MaxNicknameLength:    chat.MaxNicknameLength,
			MinNicknameLength:    chat.MinNicknameLength,
			MaxRoomIDLength:      chat.MaxRoomIDLength,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(clientConfig); err != nil {
			s.logger.Error("failed to encode config response", "err", err)
		}
	}
}

// sendSseMessage formats and sends a message according to the Server-Sent Events protocol.
func sendSseMessage(w http.ResponseWriter, data string, logger *slog.Logger) error {
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		logger.Error("failed to send SSE message", "err", err)
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

// sseHandler handles Server-Sent Events (SSE) for lobby updates.
// It registers a client for lobby updates and streams changes to the room list.
func (s *Server) sseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		validatedNickname, ok := s.handleNicknameValidation(w, r)
		if !ok {
			return // Nickname validation failed, response already sent.
		}
		defer func() {
			s.hubManager.RemoveNickname(validatedNickname)
			s.logger.Debug("SSE client disconnected, nickname released", "nickname", validatedNickname)
		}()

		s.streamEvents(w, r, validatedNickname)
	}
}

// handleNicknameValidation checks for a valid nickname and registers it.
// It returns the validated nickname and a boolean indicating success.
// If validation fails, it writes an error to the response and returns false.
func (s *Server) handleNicknameValidation(w http.ResponseWriter, r *http.Request) (string, bool) {
	nickname := r.URL.Query().Get("nickname")
	if nickname == "" {
		http.Error(w, "Nickname is required for lobby stream", http.StatusBadRequest)
		return "", false
	}

	validatedNickname, err := s.hubManager.AddNickname(nickname)
	if err != nil {
		s.logger.Info("SSE nickname registration failed", "nickname", nickname, "err", err)
		// Send a specific error event to the client to be handled by the frontend.
		if err := sendSseMessage(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), s.logger); err != nil {
			s.logger.Error("failed to send SSE error message", "err", err)
		}
		return "", false
	}
	return validatedNickname, true
}

// streamEvents handles the main SSE event streaming loop.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request, nickname string) {
	clientChan := make(chan string, 2)
	s.hubManager.RegisterLobbyClient(clientChan)
	defer s.hubManager.UnregisterLobbyClient(clientChan)

	logger := s.logger.With("nickname", nickname, "remoteAddr", r.RemoteAddr)
	logger.Info("SSE client connected to lobby stream")

	// Send initial data
	initialRooms := s.hubManager.ListRooms()
	jsonData, err := json.Marshal(initialRooms)
	if err != nil {
		logger.Error("failed to marshal initial lobby data", "err", err)
		return
	}
	if err := sendSseMessage(w, string(jsonData), logger); err != nil {
		logger.Error("failed to send initial SSE room list", "err", err)
		return
	}

	for {
		select {
		case eventData, ok := <-clientChan:
			if !ok {
				return // Hub manager closed the channel.
			}
			if err := sendSseMessage(w, eventData, logger); err != nil {
				logger.Error("failed to send SSE event data", "err", err)
				return
			}
		case <-r.Context().Done():
			logger.Info("SSE client connection closed by client request")
			return
		case <-s.shutdownChan:
			logger.Info("Server shutdown signal received. Closing SSE connection.")
			return
		}
	}
}

// wsHandler handles WebSocket connection requests.
// It upgrades the HTTP connection to a WebSocket and connects the client to the appropriate hub.
func (s *Server) wsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hub, ok := s.getHubFromRequest(w, r)
		if !ok {
			return // Error response already sent by getHubFromRequest
		}
		chat.ServeWS(hub, w, r, s.config.MaxChatMessageLength, s.config.AllowedOrigins)
	}
}

// getHubFromRequest extracts the roomID from the URL and retrieves the corresponding hub.
// It writes an error to the response if the hub is not found.
func (s *Server) getHubFromRequest(w http.ResponseWriter, r *http.Request) (*chat.Hub, bool) {
	roomID := strings.TrimPrefix(r.URL.Path, "/ws/")
	hub, err := s.hubManager.GetHub(roomID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return nil, false
	}
	return hub, true
}
