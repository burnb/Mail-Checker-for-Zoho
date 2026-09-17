package config

import (
	"errors"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	LogLevel           slog.Level
	Addr               string
	ClientID           string
	ClientSecret       string
	Host               string
	JWTSecret          string
	TokenEncryptionKey string
	TokenStorePath     string
	AllowedOrigins     string
}

func Load() (Config, error) {
	c := Config{
		LogLevel:           logLevelValue("LOG_LEVEL", "info"),
		Host:               os.Getenv("HOST"),
		Addr:               value("ADDR", ":8080"),
		ClientID:           os.Getenv("ZOHO_CLIENT_ID"),
		ClientSecret:       os.Getenv("ZOHO_CLIENT_SECRET"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		TokenEncryptionKey: os.Getenv("TOKEN_ENCRYPTION_KEY"),
		TokenStorePath:     value("TOKEN_STORE_PATH", "data/tokens.json"),
		AllowedOrigins:     os.Getenv("ALLOWED_ORIGINS"),
	}
	if c.ClientID == "" || c.ClientSecret == "" || c.Host == "" || c.JWTSecret == "" || c.TokenEncryptionKey == "" {
		return c, errors.New("ZOHO_CLIENT_ID, ZOHO_CLIENT_SECRET, HOST, JWT_SECRET, and TOKEN_ENCRYPTION_KEY are required")
	}
	if len(c.TokenEncryptionKey) != 32 {
		return c, errors.New("TOKEN_ENCRYPTION_KEY must contain exactly 32 bytes")
	}
	return c, nil
}

func value(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func logLevelValue(key, fallback string) slog.Level {
	levelStr := value(key, fallback)
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
