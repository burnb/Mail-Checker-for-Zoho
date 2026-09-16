package domain

import "context"

type Credential struct{ RefreshToken, AccountID string }
type Account struct{ ID, Email string }
type Message struct{ ID, FromName, FromEmail, Subject, Snippet, ReceivedAt, Link string }
type Folder struct {
	ID, Name string
	Unread   int
}
type CredentialRepository interface {
	Save(context.Context, string, Credential) error
	Find(context.Context, string) (Credential, error)
	FindByAccountID(context.Context, string) (string, error)
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
