package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mail-checker-server/internal/domain"
)

type ZohoClient struct {
	clientID, clientSecret, redirectURL string
	http                                *http.Client
}

func NewZohoClient(clientID, clientSecret, redirectURL string) *ZohoClient {
	return &ZohoClient{clientID, clientSecret, redirectURL, &http.Client{Timeout: 15 * time.Second}}
}
func (z *ZohoClient) AuthorizationURL(state string) string {
	query := url.Values{"response_type": {"code"}, "client_id": {z.clientID}, "redirect_uri": {z.redirectURL}, "scope": {"ZohoMail.accounts.READ,ZohoMail.folders.READ,ZohoMail.messages.READ"}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state}}
	return "https://accounts.zoho.com/oauth/v2/auth?" + query.Encode()
}
func (z *ZohoClient) ExchangeCode(ctx context.Context, code string) (domain.Credential, error) {
	data, err := z.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {z.clientID}, "client_secret": {z.clientSecret}, "redirect_uri": {z.redirectURL}})
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
	return domain.Credential{RefreshToken: data.RefreshToken, AccountID: account.ID}, nil
}
func (z *ZohoClient) RefreshAccessToken(ctx context.Context, refresh string) (string, error) {
	data, err := z.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {z.clientID}, "client_secret": {z.clientSecret}})
	return data.AccessToken, err
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
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
	var data tokenResponse
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&data) != nil || data.AccessToken == "" {
		return tokenResponse{}, errors.New("Zoho token request failed")
	}
	return data, nil
}
func (z *ZohoClient) Account(ctx context.Context, access string) (domain.Account, error) {
	data, err := z.get(ctx, access, "https://mail.zoho.com/api/accounts")
	if err != nil {
		return domain.Account{}, err
	}
	items, _ := data["data"].([]any)
	if len(items) == 0 {
		return domain.Account{}, errors.New("no Zoho Mail account")
	}
	item, _ := items[0].(map[string]any)
	id, _ := item["accountId"].(string)
	email, _ := item["emailAddress"].(string)
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
		result = append(result, domain.Message{ID: stringValue(item, "messageId"), FromName: stringValue(item, "sender"), FromEmail: stringValue(item, "fromAddress"), Subject: stringValue(item, "subject"), Snippet: stringValue(item, "summary"), ReceivedAt: stringValue(item, "receivedTime"), Link: "https://mail.zoho.com/zm/#mail/folder/inbox/p/" + stringValue(item, "messageId")})
	}
	return result, nil
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
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("Zoho returned %d", response.StatusCode)
	}
	var data map[string]any
	return data, json.NewDecoder(response.Body).Decode(&data)
}
func stringValue(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}
