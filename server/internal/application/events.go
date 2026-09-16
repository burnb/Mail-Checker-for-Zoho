package application

import (
	"context"
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

func (h *EventHub) Publish(userID, event string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients[userID] {
		select {
		case client <- event:
		default:
		}
	}
}

type WebhookService struct {
	credentials domain.CredentialRepository
	events      *EventHub
}

func NewWebhookService(credentials domain.CredentialRepository, events *EventHub) *WebhookService {
	return &WebhookService{credentials: credentials, events: events}
}

func (s *WebhookService) MailReceived(ctx context.Context, accountID string) error {
	userID, err := s.credentials.FindByAccountID(ctx, accountID)
	if err != nil {
		return ErrUnauthorized
	}
	s.events.Publish(userID, `{"type":"mail.received"}`)
	return nil
}
