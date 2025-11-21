package main

import (
	"go-chat-app/internal/server"
	"log/slog"
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
	logger := initLogger()
	slog.SetDefault(logger)

	config := loadConfig()
	app := server.New(config, logger)
	app.Start()
}

// loadConfig loads configuration from environment variables.
func loadConfig() *server.Config {
	slog.Info("Loading configuration from environment variables...")

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
		slog.Info("PORT not set, using default", "port", port)
	}

	shutdownTimeout := defaultShutdownTimeout
	if shutdownTimeoutRaw := os.Getenv("SHUTDOWN_TIMEOUT"); shutdownTimeoutRaw == "" {
		slog.Info("SHUTDOWN_TIMEOUT not set, using default", "timeout", shutdownTimeout)
	} else if parsedTimeout, err := time.ParseDuration(shutdownTimeoutRaw); err != nil {
		slog.Warn(
			"Invalid SHUTDOWN_TIMEOUT format, using default",
			"provided", shutdownTimeoutRaw,
			"error", err,
			"default", shutdownTimeout,
		)
	} else {
		shutdownTimeout = parsedTimeout
		slog.Info("SHUTDOWN_TIMEOUT loaded from environment", "timeout", shutdownTimeout)
	}

	allowedOriginsRaw := os.Getenv("ALLOWED_ORIGINS")
	slog.Debug("Read ALLOWED_ORIGINS", "rawValue", allowedOriginsRaw)
	var allowedOrigins []string
	if allowedOriginsRaw != "" {
		origins := strings.Split(allowedOriginsRaw, ",")
		for _, origin := range origins {
			allowedOrigins = append(allowedOrigins, strings.TrimSpace(origin))
		}
	} else {
		slog.Warn("ALLOWED_ORIGINS is not set. All origins will be allowed. This is insecure and should not be used in production.")
	}

	maxChatMessageLengthRaw := os.Getenv("MAX_CHAT_MESSAGE_LENGTH")
	slog.Debug("Read MAX_CHAT_MESSAGE_LENGTH", "rawValue", maxChatMessageLengthRaw)
	maxChatMessageLength, err := strconv.Atoi(maxChatMessageLengthRaw)
	if err != nil || maxChatMessageLength <= 0 {
		maxChatMessageLength = defaultMaxChatMessageLength
		slog.Info("Invalid or missing MAX_CHAT_MESSAGE_LENGTH, using default", "length", maxChatMessageLength)
	}

	slog.Info("Configuration loaded successfully",
		"port", port,
		"shutdownTimeout", shutdownTimeout,
		"allowedOrigins", allowedOrigins,
		"maxChatMessageLength", maxChatMessageLength,
	)

	return &server.Config{
		Port:                 port,
		ShutdownTimeout:      shutdownTimeout,
		AllowedOrigins:       allowedOrigins,
		MaxChatMessageLength: maxChatMessageLength,
	}
}

// initLogger initializes and returns a new slog.Logger based on the LOG_LEVEL environment variable.
func initLogger() *slog.Logger {
	var level slog.Level
	switch strings.ToUpper(os.Getenv("LOG_LEVEL")) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handlerOpts := &slog.HandlerOptions{Level: level}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, handlerOpts))

	logger.Info("Logger initialized", "level", level.String())
	return logger
}
