package transport

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	log            *slog.Logger
}

func NewHandler(
	auth *application.AuthService, mail *application.MailService, webhooks *application.WebhookService,
	events *application.EventHub, tokens domain.TokenService, webhookSecrets domain.WebhookSecretRepository,
	origins string, log *slog.Logger) *Handler {
	allowed := map[string]bool{}
	for _, origin := range strings.Split(origins, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = true
		}
	}
	return &Handler{
		auth:           auth,
		mail:           mail,
		webhooks:       webhooks,
		events:         events,
		tokens:         tokens,
		allowedOrigins: allowed,
		webhookSecrets: webhookSecrets,
		upgrader:       websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return allowed[r.Header.Get("Origin")] }},
		log:            log,
	}
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
	mux.HandleFunc("GET /mail/messages/{messageID}/content", h.authorize(h.content))
	mux.HandleFunc("PUT /mail/read", h.authorize(h.markRead))
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
	h.log.Info("WebSocket connected", "userID", userID)
	defer h.log.Info("WebSocket disconnected", "userID", userID)
	events := h.events.Subscribe(r.Context(), userID)
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	readErrors := make(chan error, 1)
	connection.SetReadDeadline(time.Now().Add(60 * time.Second))
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	go func() {
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				readErrors <- err
				return
			}
		}
	}()
	for {
		select {
		case event := <-events:
			if err := connection.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
				h.log.Error("WebSocket event delivery failed", "userID", userID, "error", err)
				return
			}
		case <-ping.C:
			if err := connection.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				h.log.Error("WebSocket ping failed", "userID", userID, "error", err)
				return
			}
		case err := <-readErrors:
			h.log.Error("WebSocket read ended", "userID", userID, "error", err)
			return
		}
	}
}

func (h *Handler) zohoWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		h.log.Error("Failed to read webhook body", "error", err)
		http.Error(w, "invalid webhook body", http.StatusBadRequest)
		return
	}

	h.log.Debug("Received Zoho webhook", "body", string(body))

	accountID := r.PathValue("accountID")
	initialized, err := h.initializeWebhookSecret(r.Context(), accountID, r.Header.Get("X-Hook-Secret"))
	if err != nil {
		h.log.Error("Zoho webhook rejected", "error", err)
		http.Error(w, "invalid webhook configuration", http.StatusUnauthorized)
		return
	}
	if initialized {
		h.log.Info("Zoho webhook secret initialized", "accountID", accountID)
		w.WriteHeader(http.StatusOK)
		return
	}
	if !validWebhookSignatureForAccount(r.Context(), h.webhookSecrets, accountID, r.Header.Get("X-Hook-Signature"), body) {
		h.log.Error("Zoho webhook rejected", "error", err)
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}

	if err := h.webhooks.MailReceived(r.Context(), accountID, body); err != nil {
		h.log.Error("Zoho webhook processing failed", "accountID", accountID, "error", err)
		http.Error(w, "unknown account", http.StatusNotFound)
		return
	}
	h.log.Debug("Zoho webhook accepted", "accountID", accountID)
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
		h.log.Error("OAuth callback failed", "error", err)
		http.Error(w, "OAuth failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	redirect, err := url.Parse(callback)
	if err != nil {
		h.log.Error("Invalid OAuth callback", "error", err)
		http.Error(w, "invalid callback", http.StatusInternalServerError)
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
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
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
	writeJSON(w, http.StatusOK, map[string]int{"unread": len(messages)})
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
	writeJSON(w, http.StatusOK, map[string]any{"items": messages, "account": map[string]string{"email": email, "id": accountID}})
}

func (h *Handler) content(w http.ResponseWriter, r *http.Request, userID string) {
	content, err := h.mail.Content(r.Context(), userID, r.URL.Query().Get("folderId"), r.PathValue("messageID"))
	if err != nil {
		h.mailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"html": content})
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request, userID string) {
	var request struct {
		MessageIDs []string `json:"messageIds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil || len(request.MessageIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "messageIds is required"})
		return
	}
	if err := h.mail.MarkRead(r.Context(), userID, request.MessageIDs); err != nil {
		h.mailError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) folders(w http.ResponseWriter, r *http.Request, userID string) {
	folders, err := h.mail.Folders(r.Context(), userID)
	if err != nil {
		h.mailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (h *Handler) mailError(w http.ResponseWriter, err error) {
	if err == application.ErrUnauthorized {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	h.log.Error("Zoho Mail request failed", "error", err)
	http.Error(w, "Zoho Mail request failed: "+err.Error(), http.StatusBadGateway)
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

func (h *Handler) initializeWebhookSecret(ctx context.Context, accountID, receivedSecret string) (bool, error) {
	secret, err := h.webhookSecrets.WebhookSecret(ctx, accountID)
	if err != nil {
		return false, err
	}
	if secret != "" {
		return false, nil
	}
	if receivedSecret == "" {
		return false, errors.New("missing X-Hook-Secret while initializing webhook")
	}
	if err := h.webhookSecrets.SaveWebhookSecret(ctx, accountID, receivedSecret); err != nil {
		return false, err
	}
	return true, nil
}

func validWebhookSignatureForAccount(ctx context.Context, secrets domain.WebhookSecretRepository, accountID, signature string, body []byte) bool {
	secret, err := secrets.WebhookSecret(ctx, accountID)
	if err != nil || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	provided, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		provided, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(signature))
	}
	return err == nil && hmac.Equal(provided, mac.Sum(nil))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
