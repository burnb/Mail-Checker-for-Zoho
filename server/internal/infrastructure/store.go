package infrastructure

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"mail-checker-server/internal/domain"
)

type FileCredentialStore struct {
	mu             sync.RWMutex
	path           string
	block          cipher.Block
	records        map[string]domain.Credential
	webhookSecrets map[string]string
}

type persistedStore struct {
	Credentials    map[string]domain.Credential `json:"credentials"`
	WebhookSecrets map[string]string            `json:"webhookSecrets"`
}

func NewFileCredentialStore(path, key string) (*FileCredentialStore, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	store := &FileCredentialStore{path: path, block: block, records: map[string]domain.Credential{}, webhookSecrets: map[string]string{}}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var persisted persistedStore
	if err := json.Unmarshal(content, &persisted); err != nil {
		return nil, err
	}
	if persisted.Credentials == nil {
		if err := json.Unmarshal(content, &store.records); err != nil {
			return nil, err
		}
	} else {
		store.records = persisted.Credentials
		for accountID, encryptedSecret := range persisted.WebhookSecrets {
			secret, err := store.decrypt(encryptedSecret)
			if err != nil {
				return nil, err
			}
			store.webhookSecrets[accountID] = secret
		}
	}
	for id, credential := range store.records {
		plain, err := store.decrypt(credential.RefreshToken)
		if err != nil {
			return nil, err
		}
		credential.RefreshToken = plain
		store.records[id] = credential
	}
	return store, nil
}
func (s *FileCredentialStore) Save(_ context.Context, id string, credential domain.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[id] = credential
	return s.persist()
}
func (s *FileCredentialStore) persist() error {
	persisted := map[string]domain.Credential{}
	for id, record := range s.records {
		encrypted, err := s.encrypt(record.RefreshToken)
		if err != nil {
			return err
		}
		record.RefreshToken = encrypted
		persisted[id] = record
	}
	storedSecrets := map[string]string{}
	for accountID, secret := range s.webhookSecrets {
		encryptedSecret, err := s.encrypt(secret)
		if err != nil {
			return err
		}
		storedSecrets[accountID] = encryptedSecret
	}
	data, err := json.Marshal(persistedStore{Credentials: persisted, WebhookSecrets: storedSecrets})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}
func (s *FileCredentialStore) Find(_ context.Context, id string) (domain.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	credential, ok := s.records[id]
	if !ok {
		return domain.Credential{}, errors.New("credential not found")
	}
	return credential, nil
}
func (s *FileCredentialStore) FindByAccountID(_ context.Context, accountID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for userID, credential := range s.records {
		if credential.AccountID == accountID {
			return userID, nil
		}
	}
	return "", errors.New("credential not found")
}
func (s *FileCredentialStore) WebhookSecret(_ context.Context, accountID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.webhookSecrets[accountID], nil
}
func (s *FileCredentialStore) SaveWebhookSecret(_ context.Context, accountID, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.webhookSecrets[accountID] = secret
	return s.persist()
}
func (s *FileCredentialStore) encrypt(value string) (string, error) {
	gcm, err := cipher.NewGCM(s.block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, []byte(value), nil)...)), nil
}
func (s *FileCredentialStore) decrypt(value string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(s.block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted token")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(plain), err
}
