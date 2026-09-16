package infrastructure

import (
	"testing"
	"time"
)

func TestJWTTokenServiceIssuesVerifiableTokens(t *testing.T) {
	service := NewJWTTokenService("test-secret")
	token, err := service.Issue(map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := service.Verify(token)
	if err != nil || claims["sub"] != "user-1" {
		t.Fatalf("claims=%v err=%v", claims, err)
	}
}

func TestJWTTokenServiceRejectsInvalidToken(t *testing.T) {
	if _, err := NewJWTTokenService("test-secret").Verify("not-a-jwt"); err == nil {
		t.Fatal("invalid token was accepted")
	}
}
