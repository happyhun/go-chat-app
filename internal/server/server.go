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
	config       *Config
	hubManager   *chat.HubManager
	router       *http.ServeMux
	shutdownChan chan struct{}
	httpServer   *http.Server
}

// New creates and initializes a new Server instance.
// It registers routes and returns the server.
func New(config *Config) *Server {
	hubManager := chat.NewHubManager()
	server := &Server{
		config:       config,
		hubManager:   hubManager,
		router:       http.NewServeMux(),
		shutdownChan: make(chan struct{}),
	}
	server.httpServer = &http.Server{
		Addr:    ":" + config.Port,
		Handler: server.cspMiddleware(server.router),
	}
	server.registerRoutes()
	return server
}

// Start begins the HTTP server and waits for a signal to initiate a graceful shutdown.
func (s *Server) Start() {
	// Run the server in a separate goroutine so that it doesn't block.
	go func() {
		log.Printf("Server starting on http://localhost:%s", s.config.Port)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	s.shutdown()
}

// shutdown orchestrates the graceful shutdown of the server.
func (s *Server) shutdown() {
	log.Println("Shutting down server...")

	// 1. Signal all long-running HTTP handlers (like SSE) to shut down.
	// This must be done BEFORE shutting down hubs, which might generate events that
	// could cause a deadlock in the SSE handler.
	close(s.shutdownChan)

	// 2. Shut down all WebSocket hubs.
	s.hubManager.ShutdownAllHubs()

	// 3. Shut down the HTTP server itself, waiting for handlers to finish.
	ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited properly")
}
