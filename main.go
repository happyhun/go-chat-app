package main

import (
	"go-chat-app/internal/server"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort                 = "8080"
	defaultShutdownTimeout      = 5 * time.Second
	defaultMaxChatMessageLength = 1000
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
		port = defaultPort
	}

	shutdownTimeoutRaw := os.Getenv("SHUTDOWN_TIMEOUT")
	shutdownTimeout, err := time.ParseDuration(shutdownTimeoutRaw)
	if err != nil || shutdownTimeoutRaw == "" {
		shutdownTimeout = defaultShutdownTimeout
		log.Printf("WARN: Invalid or missing SHUTDOWN_TIMEOUT. Using default: %v", shutdownTimeout)
	}

	allowedOriginsRaw := os.Getenv("ALLOWED_ORIGINS")
	var allowedOrigins []string
	if allowedOriginsRaw != "" {
		origins := strings.Split(allowedOriginsRaw, ",")
		for _, origin := range origins {
			allowedOrigins = append(allowedOrigins, strings.TrimSpace(origin))
		}
	}

	maxChatMessageLengthRaw := os.Getenv("MAX_CHAT_MESSAGE_LENGTH")
	maxChatMessageLength, err := strconv.Atoi(maxChatMessageLengthRaw)
	if err != nil || maxChatMessageLength <= 0 {
		maxChatMessageLength = defaultMaxChatMessageLength
		log.Printf("WARN: Invalid or missing MAX_CHAT_MESSAGE_LENGTH. Using default: %d", maxChatMessageLength)
	}

	log.Printf("Configuration loaded: Port=%s, ShutdownTimeout=%v, MaxChatMessageLength=%d", port, shutdownTimeout, maxChatMessageLength)
	if len(allowedOrigins) == 0 {
		log.Printf("WARN: ALLOWED_ORIGINS is not set. All origins will be allowed. This is insecure and should not be used in production.")
	} else {
		log.Printf("AllowedOrigins: %v", allowedOrigins)
	}

	return &server.Config{
		Port:                 port,
		ShutdownTimeout:      shutdownTimeout,
		AllowedOrigins:       allowedOrigins,
		MaxChatMessageLength: maxChatMessageLength,
	}
}
