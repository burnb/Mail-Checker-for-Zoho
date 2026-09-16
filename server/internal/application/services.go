package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"mail-checker-server/internal/domain"
)

var ErrUnauthorized = errors.New("unauthorized")

type AuthService struct {
	credentials domain.CredentialRepository
	tokens      domain.TokenService
	zoho        domain.ZohoGateway
}

func NewAuthService(credentials domain.CredentialRepository, tokens domain.TokenService, zoho domain.ZohoGateway) *AuthService {
	return &AuthService{credentials, tokens, zoho}
}
func (s *AuthService) Start(sessionID, callbackURL string) (string, error) {
	if sessionID == "" || callbackURL == "" {
		return "", errors.New("session_id and extension_callback are required")
	}
	state, err := s.tokens.Issue(map[string]any{"sid": sessionID, "cb": callbackURL, "exp": time.Now().Add(10 * time.Minute).Unix()})
	if err != nil {
		return "", err
	}
	return s.zoho.AuthorizationURL(state), nil
}
func (s *AuthService) Complete(ctx context.Context, state, code string) (string, string, error) {
	claims, err := s.tokens.Verify(state)
	if err != nil {
		return "", "", err
	}
	callbackURL, _ := claims["cb"].(string)
	if callbackURL == "" || code == "" {
		return "", "", errors.New("invalid OAuth response")
	}
	credential, err := s.zoho.ExchangeCode(ctx, code)
	if err != nil {
		return "", "", err
	}
	userID, err := identifier()
	if err != nil {
		return "", "", err
	}
	if err := s.credentials.Save(ctx, userID, credential); err != nil {
		return "", "", err
	}
	token, err := s.tokens.Issue(map[string]any{"sub": userID, "exp": time.Now().Add(30 * 24 * time.Hour).Unix()})
	return callbackURL, token, err
}

type MailService struct {
	credentials domain.CredentialRepository
	zoho        domain.ZohoGateway
}

func NewMailService(credentials domain.CredentialRepository, zoho domain.ZohoGateway) *MailService {
	return &MailService{credentials, zoho}
}
func (s *MailService) Unread(ctx context.Context, userID string) ([]domain.Message, error) {
	access, credential, err := s.access(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.zoho.UnreadMessages(ctx, access, credential.AccountID, "", 100)
}
func (s *MailService) List(ctx context.Context, userID, folder string, limit int) ([]domain.Message, error) {
	access, credential, err := s.access(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.zoho.UnreadMessages(ctx, access, credential.AccountID, folder, limit)
}
func (s *MailService) Folders(ctx context.Context, userID string) ([]domain.Folder, error) {
	access, credential, err := s.access(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.zoho.Folders(ctx, access, credential.AccountID)
}
func (s *MailService) access(ctx context.Context, userID string) (string, domain.Credential, error) {
	credential, err := s.credentials.Find(ctx, userID)
	if err != nil {
		return "", domain.Credential{}, ErrUnauthorized
	}
	access, err := s.zoho.RefreshAccessToken(ctx, credential.RefreshToken)
	return access, credential, err
}
func identifier() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
