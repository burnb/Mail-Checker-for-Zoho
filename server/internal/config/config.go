package config

import (
	"errors"
	"os"
)

type Config struct {
	Addr,
	ClientID,
	ClientSecret,
	RedirectURL,
	JWTSecret,
	TokenEncryptionKey,
	TokenStorePath,
	AllowedOrigins,
	WebhookSecret string
}

func Load() (Config, error) {
	c := Config{
		Addr:               value("ADDR", ":8080"),
		ClientID:           os.Getenv("ZOHO_CLIENT_ID"),
		ClientSecret:       os.Getenv("ZOHO_CLIENT_SECRET"),
		RedirectURL:        os.Getenv("ZOHO_REDIRECT_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		TokenEncryptionKey: os.Getenv("TOKEN_ENCRYPTION_KEY"),
		TokenStorePath:     value("TOKEN_STORE_PATH", "data/tokens.json"),
		AllowedOrigins:     os.Getenv("ALLOWED_ORIGINS"),
		WebhookSecret:      os.Getenv("WEBHOOK_SECRET"),
	}
	if c.ClientID == "" || c.ClientSecret == "" || c.RedirectURL == "" || c.JWTSecret == "" || c.TokenEncryptionKey == "" || c.WebhookSecret == "" {
		return c, errors.New("ZOHO_CLIENT_ID, ZOHO_CLIENT_SECRET, ZOHO_REDIRECT_URL, JWT_SECRET, TOKEN_ENCRYPTION_KEY, and WEBHOOK_SECRET are required")
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
