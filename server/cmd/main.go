package main

import (
	"log"
	"net/http"

	"mail-checker-server/internal/application"
	"mail-checker-server/internal/config"
	"mail-checker-server/internal/infrastructure"
	"mail-checker-server/internal/transport"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	store, err := infrastructure.NewFileCredentialStore(cfg.TokenStorePath, cfg.TokenEncryptionKey)
	if err != nil {
		log.Fatal(err)
	}
	tokens := infrastructure.NewJWTTokenService(cfg.JWTSecret)
	zoho := infrastructure.NewZohoClient(cfg.ClientID, cfg.ClientSecret, cfg.RedirectURL)
	app := application.NewAuthService(store, tokens, zoho)
	mail := application.NewMailService(store, zoho)
	events := application.NewEventHub()
	webhooks := application.NewWebhookService(store, events)
	handler := transport.NewHandler(app, mail, webhooks, events, tokens, cfg.AllowedOrigins, cfg.WebhookSecret)
	log.Printf("Mail Checker API listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, handler.Routes()))
}
