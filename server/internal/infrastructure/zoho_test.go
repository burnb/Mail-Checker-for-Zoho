package infrastructure

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestZohoClientMarkReadUsesDocumentedRequest(t *testing.T) {
	client := &ZohoClient{
		http: &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPut {
				t.Errorf("method = %s, want PUT", request.Method)
			}
			if request.URL.Path != "/api/accounts/account-1/updatemessage" {
				t.Errorf("path = %s, want /api/accounts/account-1/updatemessage", request.URL.Path)
			}
			if request.Header.Get("Authorization") != "Zoho-oauthtoken access-token" {
				t.Errorf("authorization = %q", request.Header.Get("Authorization"))
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `{"messageId":["message-1","message-2"],"mode":"markAsRead"}` {
				t.Errorf("body = %s", body)
			}
			return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		})},
	}

	if err := client.MarkRead(context.Background(), "access-token", "account-1", []string{"message-1", "message-2"}); err != nil {
		t.Fatal(err)
	}
}
