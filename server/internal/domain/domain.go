package domain

import "context"

type Credential struct{ RefreshToken, AccountID, Email string }
type Account struct{ ID, Email string }
type Message struct {
	ID         string `json:"id"`
	FromName   string `json:"fromName"`
	FromEmail  string `json:"fromEmail"`
	Subject    string `json:"subject"`
	Snippet    string `json:"snippet"`
	ReceivedAt string `json:"receivedAt"`
	Link       string `json:"link"`
}
type Folder struct {
	ID, Name string
	Unread   int
}
type CredentialRepository interface {
	Save(context.Context, string, Credential) error
	Find(context.Context, string) (Credential, error)
	FindByAccountID(context.Context, string) (string, error)
}
type WebhookSecretRepository interface {
	WebhookSecret(context.Context, string) (string, error)
	SaveWebhookSecret(context.Context, string, string) error
}
type TokenService interface {
	Issue(map[string]any) (string, error)
	Verify(string) (map[string]any, error)
}
type ZohoGateway interface {
	AuthorizationURL(string) string
	ExchangeCode(context.Context, string) (Credential, error)
	RefreshAccessToken(context.Context, string) (string, error)
	Account(context.Context, string) (Account, error)
	UnreadMessages(context.Context, string, string, string, int) ([]Message, error)
	Folders(context.Context, string, string) ([]Folder, error)
}
