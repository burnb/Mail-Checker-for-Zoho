package transport

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mail-checker-server/internal/application"
	"mail-checker-server/internal/domain"

	"github.com/gorilla/websocket"
)

type Handler struct {
	auth           *application.AuthService
	mail           *application.MailService
	webhooks       *application.WebhookService
	events         *application.EventHub
	tokens         domain.TokenService
	allowedOrigins map[string]bool
	webhookSecrets domain.WebhookSecretRepository
	upgrader       websocket.Upgrader
}

func NewHandler(auth *application.AuthService, mail *application.MailService, webhooks *application.WebhookService, events *application.EventHub, tokens domain.TokenService, webhookSecrets domain.WebhookSecretRepository, origins string) *Handler {
	allowed := map[string]bool{}
	for _, origin := range strings.Split(origins, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = true
		}
	}
	return &Handler{auth: auth, mail: mail, webhooks: webhooks, events: events, tokens: tokens, allowedOrigins: allowed, webhookSecrets: webhookSecrets, upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return allowed[r.Header.Get("Origin")] }}}
}
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /auth/zoho", h.startAuth)
	mux.HandleFunc("GET /auth/zoho/callback", h.finishAuth)
	mux.HandleFunc("GET /events", h.eventsSocket)
	mux.HandleFunc("POST /webhooks/zoho/{accountID}", h.zohoWebhook)
	mux.HandleFunc("GET /mail/unread", h.authorize(h.unread))
	mux.HandleFunc("GET /mail/unread/list", h.authorize(h.list))
	mux.HandleFunc("GET /mail/folders", h.authorize(h.folders))
	return h.cors(mux)
}
func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (h *Handler) eventsSocket(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authorizedUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	connection, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer connection.Close()
	log.Printf("WebSocket connected for user %s", userID)
	defer log.Printf("WebSocket disconnected for user %s", userID)
	events := h.events.Subscribe(r.Context(), userID)
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case event := <-events:
			if err := connection.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
				return
			}
		case <-ping.C:
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}
func (h *Handler) zohoWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid webhook body", http.StatusBadRequest)
		return
	}
	accountID := r.PathValue("accountID")
	if err := h.validateWebhookSignature(r.Context(), accountID, r.Header.Get("X-Hook-Secret"), r.Header.Get("X-Hook-Signature"), body); err != nil {
		log.Printf("Zoho webhook rejected: %v", err)
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}
	if err := h.webhooks.MailReceived(r.Context(), accountID); err != nil {
		http.Error(w, "unknown account", http.StatusNotFound)
		return
	}
	log.Printf("Zoho webhook accepted for account %s", accountID)
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) startAuth(w http.ResponseWriter, r *http.Request) {
	callback := r.URL.Query().Get("extension_callback")
	if !h.validExtensionCallback(callback) {
		http.Error(w, "invalid extension_callback", http.StatusBadRequest)
		return
	}
	authorizationURL, err := h.auth.Start(r.URL.Query().Get("session_id"), callback)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, authorizationURL, http.StatusFound)
}
func (h *Handler) finishAuth(w http.ResponseWriter, r *http.Request) {
	callback, token, err := h.auth.Complete(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil {
		log.Printf("OAuth callback failed: %v", err)
		http.Error(w, "OAuth failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	redirect, err := url.Parse(callback)
	if err != nil {
		http.Error(w, "invalid callback", 500)
		return
	}
	query := redirect.Query()
	query.Set("token", token)
	redirect.RawQuery = query.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}
func (h *Handler) authorize(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := h.authorizedUser(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r, userID)
	}
}
func (h *Handler) authorizedUser(r *http.Request) (string, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("access_token")
	}
	claims, err := h.tokens.Verify(token)
	if err != nil {
		return "", false
	}
	userID, ok := claims["sub"].(string)
	return userID, ok && userID != ""
}
func (h *Handler) unread(w http.ResponseWriter, r *http.Request, userID string) {
	messages, err := h.mail.Unread(r.Context(), userID)
	if err != nil {
		h.mailError(w, err)
		return
	}
	writeJSON(w, 200, map[string]int{"unread": len(messages)})
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request, userID string) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	messages, err := h.mail.List(r.Context(), userID, r.URL.Query().Get("folder"), limit)
	if err != nil {
		h.mailError(w, err)
		return
	}
	email, err := h.mail.AccountEmail(r.Context(), userID)
	if err != nil {
		h.mailError(w, err)
		return
	}
	accountID, err := h.mail.AccountID(r.Context(), userID)
	if err != nil {
		h.mailError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": messages, "account": map[string]string{"email": email, "id": accountID}})
}
func (h *Handler) folders(w http.ResponseWriter, r *http.Request, userID string) {
	folders, err := h.mail.Folders(r.Context(), userID)
	if err != nil {
		h.mailError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"folders": folders})
}
func (h *Handler) mailError(w http.ResponseWriter, err error) {
	if err == application.ErrUnauthorized {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	http.Error(w, "Zoho Mail request failed", 502)
}
func (h *Handler) validExtensionCallback(callback string) bool {
	parsed, err := url.Parse(callback)
	return err == nil && (parsed.Scheme == "chrome-extension" || parsed.Scheme == "moz-extension") && strings.HasSuffix(parsed.Path, "/callback.html")
}
func (h *Handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.allowedOrigins[r.Header.Get("Origin")] {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Vary", "Origin")
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) validateWebhookSignature(ctx context.Context, accountID, receivedSecret, signature string, body []byte) error {
	secret, err := h.webhookSecrets.WebhookSecret(ctx, accountID)
	if err != nil {
		return err
	}
	if secret == "" {
		if receivedSecret == "" {
			return errors.New("missing X-Hook-Secret while initializing webhook")
		}
		if err := h.webhookSecrets.SaveWebhookSecret(ctx, accountID, receivedSecret); err != nil {
			return err
		}
		secret = receivedSecret
		log.Print("Zoho webhook secret initialized")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	provided, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, expected) {
		return errors.New("X-Hook-Signature does not match")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
