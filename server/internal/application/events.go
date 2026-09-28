package application

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"mail-checker-server/internal/domain"
)

type EventHub struct {
	mu      sync.RWMutex
	clients map[string]map[chan string]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{clients: make(map[string]map[chan string]struct{})}
}

func (h *EventHub) Subscribe(ctx context.Context, userID string) <-chan string {
	events := make(chan string, 1)
	h.mu.Lock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[chan string]struct{})
	}
	h.clients[userID][events] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.clients[userID], events)
		if len(h.clients[userID]) == 0 {
			delete(h.clients, userID)
		}
		h.mu.Unlock()
	}()
	return events
}

func (h *EventHub) Publish(userID, event string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	delivered := 0
	for client := range h.clients[userID] {
		select {
		case client <- event:
			delivered++
		default:
		}
	}
	return delivered
}

type WebhookService struct {
	credentials domain.CredentialRepository
	events      *EventHub
	log         *slog.Logger
}

func NewWebhookService(credentials domain.CredentialRepository, events *EventHub, log *slog.Logger) *WebhookService {
	return &WebhookService{credentials: credentials, events: events, log: log}
}

func (s *WebhookService) MailReceived(ctx context.Context, accountID string, data []byte) error {
	userID, err := s.credentials.FindByAccountID(ctx, accountID)
	if err != nil {
		return ErrUnauthorized
	}
	delivered := s.events.Publish(userID, fmt.Sprintf(`{"type":"mail.received", "mail": %s}`, string(data)))
	if delivered == 0 {
		s.log.Warn("mail.received event had no connected WebSocket subscribers", "accountID", accountID, "userID", userID)
	}
	return nil
}
