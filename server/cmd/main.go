package main

import (
	"log/slog"
	"net/http"
	"os"

	"mail-checker-server/internal/application"
	"mail-checker-server/internal/config"
	"mail-checker-server/internal/infrastructure"
	"mail-checker-server/internal/transport"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	store, err := infrastructure.NewFileCredentialStore(cfg.TokenStorePath, cfg.TokenEncryptionKey)
	if err != nil {
		log.Error("Failed to create credential store", "error", err)
		os.Exit(1)
	}
	tokens := infrastructure.NewJWTTokenService(cfg.JWTSecret)
	zoho := infrastructure.NewZohoClient(cfg.ClientID, cfg.ClientSecret, cfg.Host, log)
	app := application.NewAuthService(cfg.Host, store, tokens, zoho, log)
	mail := application.NewMailService(store, zoho)
	events := application.NewEventHub()
	webhooks := application.NewWebhookService(store, events)
	handler := transport.NewHandler(app, mail, webhooks, events, tokens, store, cfg.AllowedOrigins, log)
	log.Info("Mail Checker API listening on " + cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, handler.Routes()); err != nil {
		log.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}
