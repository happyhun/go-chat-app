package main

import (
	"go-chat-app/internal/server"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	config := loadConfig()
	app := server.New(config)
	app.Start()
}

// loadConfig loads configuration from environment variables.
func loadConfig() *server.Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	shutdownTimeout, err := time.ParseDuration(os.Getenv("SHUTDOWN_TIMEOUT"))
	if err != nil {
		shutdownTimeout = 5 * time.Second
	}

	allowedOriginsEnv := os.Getenv("ALLOWED_ORIGINS")
	var allowedOrigins []string
	if allowedOriginsEnv != "" {
		origins := strings.Split(allowedOriginsEnv, ",")
		for _, origin := range origins {
			allowedOrigins = append(allowedOrigins, strings.TrimSpace(origin))
		}
	}

	log.Printf("Configuration loaded: Port=%s, ShutdownTimeout=%v", port, shutdownTimeout)
	if len(allowedOrigins) == 0 {
		log.Printf("WARN: ALLOWED_ORIGINS is not set. All origins will be allowed. This is insecure and should not be used in production.")
	} else {
		log.Printf("AllowedOrigins: %v", allowedOrigins)
	}

	return &server.Config{
		Port:                 port,
		ShutdownTimeout:      shutdownTimeout,
		AllowedOrigins:       allowedOrigins,
		MaxChatMessageLength: 1000, // Default max chat message length
	}
}
