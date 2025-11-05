package server

import (
	"context"
	"errors"
	"go-chat-app/internal/chat"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Config holds the server configuration.
type Config struct {
	Port                 string
	ShutdownTimeout      time.Duration
	AllowedOrigins       []string // List of allowed origins for WebSocket connections
	MaxChatMessageLength int
}

// Server manages the application's core dependencies (config, hub manager, router).
type Server struct {
	config     *Config
	hubManager *chat.HubManager
	router     *http.ServeMux
}

// New creates and initializes a new Server instance.
// It registers routes and returns the server.
func New(config *Config) *Server {
	hubManager := chat.NewHubManager()
	server := &Server{
		config:     config,
		hubManager: hubManager,
		router:     http.NewServeMux(),
	}
	server.registerRoutes()
	return server
}

// Start begins the HTTP server and handles graceful shutdown.
func (s *Server) Start() {
	server := &http.Server{
		Addr:    ":" + s.config.Port,
		Handler: s.cspMiddleware(s.router),
	}

	go func() {
		log.Printf("Server starting on http://localhost:%s", s.config.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Notify all clients and safely shut down all hubs
	s.hubManager.ShutdownAllHubs()

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited properly")
}
