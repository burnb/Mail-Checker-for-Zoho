package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mail-checker-server/internal/domain"
)

type ZohoClient struct {
	clientID, clientSecret, host string
	http                         *http.Client
	log                          *slog.Logger
}

func NewZohoClient(clientID, clientSecret, host string, log *slog.Logger) *ZohoClient {
	return &ZohoClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		host:         host,
		http:         &http.Client{Timeout: 15 * time.Second},
		log:          log,
	}
}

func (z *ZohoClient) AuthorizationURL(state string) string {
	query := url.Values{
		"response_type": {"code"},
		"client_id":     {z.clientID},
		"redirect_uri":  {fmt.Sprintf("%s/auth/zoho/callback", z.host)},
		"scope":         {"ZohoMail.accounts.READ,ZohoMail.folders.READ,ZohoMail.messages.READ,ZohoMail.messages.UPDATE"},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return "https://accounts.zoho.com/oauth/v2/auth?" + query.Encode()
}

func (z *ZohoClient) ExchangeCode(ctx context.Context, code string) (domain.Credential, error) {
	data, err := z.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {z.clientID},
		"client_secret": {z.clientSecret},
		"redirect_uri":  {fmt.Sprintf("%s/auth/zoho/callback", z.host)},
	})
	if err != nil {
		return domain.Credential{}, err
	}

	account, err := z.Account(ctx, data.AccessToken)
	if err != nil {
		return domain.Credential{}, err
	}
	if data.RefreshToken == "" {
		return domain.Credential{}, errors.New("Zoho omitted refresh token")

	}
	return domain.Credential{RefreshToken: data.RefreshToken, AccountID: account.ID, Email: account.Email}, nil
}

func (z *ZohoClient) RefreshAccessToken(ctx context.Context, refresh string) (string, error) {
	data, err := z.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {z.clientID}, "client_secret": {z.clientSecret}})
	return data.AccessToken, err
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func (z *ZohoClient) token(ctx context.Context, form url.Values) (tokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://accounts.zoho.com/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := z.http.Do(request)
	if err != nil {
		return tokenResponse{}, err
	}
	defer response.Body.Close()

	z.log.Debug("Zoho token response received", "status", response.StatusCode, "body", response.Body)

	var data tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
		return tokenResponse{}, fmt.Errorf("decode Zoho token response: %w", err)
	}

	if response.StatusCode != http.StatusOK || data.AccessToken == "" {
		if data.Error != "" {
			if data.Description != "" {
				return tokenResponse{}, fmt.Errorf("Zoho token request failed: %s (%s)", data.Error, data.Description)
			}
			return tokenResponse{}, fmt.Errorf("Zoho token request failed: %s", data.Error)
		}
		return tokenResponse{}, fmt.Errorf("Zoho token request failed with status %d", response.StatusCode)
	}

	return data, nil
}

func (z *ZohoClient) Account(ctx context.Context, access string) (domain.Account, error) {
	data, err := z.get(ctx, access, "https://mail.zoho.com/api/accounts")
	if err != nil {
		return domain.Account{}, err
	}

	z.log.Debug("fetched Zoho account data", "data", data)

	items, _ := data["data"].([]any)
	if len(items) == 0 {
		return domain.Account{}, errors.New("no Zoho Mail account")
	}
	item, _ := items[0].(map[string]any)
	id, _ := item["accountId"].(string)
	email, _ := item["primaryEmailAddress"].(string)
	if id == "" {
		return domain.Account{}, errors.New("missing account ID")
	}

	return domain.Account{ID: id, Email: email}, nil
}

func (z *ZohoClient) UnreadMessages(ctx context.Context, access, accountID, folder string, limit int) ([]domain.Message, error) {
	endpoint := "https://mail.zoho.com/api/accounts/" + url.PathEscape(accountID) + "/messages/view?status=unread&limit=" + strconv.Itoa(limit)
	if folder != "" {
		endpoint += "&folderId=" + url.QueryEscape(folder)
	}
	data, err := z.get(ctx, access, endpoint)
	if err != nil {
		return nil, err
	}
	items, _ := data["data"].([]any)
	result := make([]domain.Message, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		result = append(result, domain.Message{ID: stringValue(item, "messageId"), FolderID: stringValue(item, "folderId"), FromName: stringValue(item, "sender"), FromEmail: stringValue(item, "fromAddress"), Subject: stringValue(item, "subject"), Snippet: stringValue(item, "summary"), ReceivedAt: stringValue(item, "receivedTime"), Link: "https://mail.zoho.com/zm/#mail/folder/inbox/p/" + stringValue(item, "messageId")})
	}
	return result, nil
}

func (z *ZohoClient) MessageContent(ctx context.Context, access, accountID, folderID, messageID string) (string, error) {
	if folderID == "" || messageID == "" {
		return "", errors.New("folder ID and message ID are required")
	}
	endpoint := "https://mail.zoho.com/api/accounts/" + url.PathEscape(accountID) + "/folders/" + url.PathEscape(folderID) + "/messages/" + url.PathEscape(messageID) + "/content"
	data, err := z.get(ctx, access, endpoint)
	if err != nil {
		return "", err
	}
	contentData, _ := data["data"].(map[string]any)
	content := stringValue(contentData, "content")
	if content == "" {
		return "", errors.New("Zoho response did not include message content")
	}
	return content, nil
}

func (z *ZohoClient) MarkRead(ctx context.Context, access, accountID string, messageIDs []string) error {
	if len(messageIDs) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{"messageId": messageIDs, "mode": "markAsRead"})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, "https://mail.zoho.com/api/accounts/"+url.PathEscape(accountID)+"/updatemessage", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Zoho-oauthtoken "+access)
	request.Header.Set("Content-Type", "application/json")
	response, err := z.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode > http.StatusNoContent {
		return fmt.Errorf("Zoho returned %d", response.StatusCode)
	}
	return nil
}

func (z *ZohoClient) Folders(ctx context.Context, access, accountID string) ([]domain.Folder, error) {
	data, err := z.get(ctx, access, "https://mail.zoho.com/api/accounts/"+url.PathEscape(accountID)+"/folders")
	if err != nil {
		return nil, err
	}
	items, _ := data["data"].([]any)
	result := make([]domain.Folder, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		result = append(result, domain.Folder{ID: stringValue(item, "folderId"), Name: stringValue(item, "folderName")})
	}
	return result, nil
}

func (z *ZohoClient) get(ctx context.Context, access, endpoint string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Zoho-oauthtoken "+access)
	response, err := z.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	z.log.Debug("Zoho GET response", "status", response.StatusCode, "endpoint", endpoint, "body", response.Body)

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("Zoho returned %d", response.StatusCode)
	}

	var data map[string]any
	return data, json.NewDecoder(response.Body).Decode(&data)
}

func stringValue(data map[string]any, key string) string {
	switch value := data[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatInt(int64(value), 10)
	default:
		return ""
	}
}
