package server

import (
	"bufio"
	"context"
	"errors"
	"go-chat-app/internal/chat"
	"log/slog"
	"net"
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
	logger       *slog.Logger
}

// New creates and initializes a new Server instance.
// It registers routes and returns the server.
func New(config *Config, logger *slog.Logger) *Server {
	hubManager := chat.NewHubManager(logger)
	server := &Server{
		config:       config,
		hubManager:   hubManager,
		router:       http.NewServeMux(),
		shutdownChan: make(chan struct{}),
		logger:       logger,
	}
	server.httpServer = &http.Server{
		Addr:    ":" + config.Port,
		Handler: server.loggingMiddleware(server.cspMiddleware(server.router)),
	}
	server.registerRoutes()
	return server
}

// Start begins the HTTP server and waits for a signal to initiate a graceful shutdown.
func (s *Server) Start() {
	// Run the server in a separate goroutine so that it doesn't block.
	go func() {
		s.logger.Info("Server starting", "addr", "http://localhost:"+s.config.Port)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("Failed to start server", "err", err)
			os.Exit(1)
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
	s.logger.Info("Shutting down server...")

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
		s.logger.Error("Server forced to shutdown due to timeout", "err", err)
		os.Exit(1)
	}

	s.logger.Info("Server exited properly")
}

// responseWriter is a wrapper around http.ResponseWriter that allows us to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	// Default to 200 OK if WriteHeader is not called.
	return &responseWriter{w, http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements the http.Hijacker interface to support WebSocket upgrades.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("http.Hijacker interface is not supported")
	}
	return h.Hijack()
}

// Flush implements the http.Flusher interface to support streaming responses like SSE.
func (rw *responseWriter) Flush() {
	// We need to check if the underlying ResponseWriter supports the Flusher interface.
	// This is crucial for streaming responses.
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	// If it doesn't support it, we do nothing, which is the correct behavior.
}

// loggingMiddleware logs details about each incoming HTTP request.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)
		next.ServeHTTP(rw, r)
		s.logger.Info("HTTP request processed",
			"method", r.Method,
			"path", r.URL.Path,
			"remoteAddr", r.RemoteAddr,
			"status", rw.statusCode,
			"duration", time.Since(start),
		)
	})
}
