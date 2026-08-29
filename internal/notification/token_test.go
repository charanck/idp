package notification_test

import (
	"testing"

	"github.com/google/uuid"

	"controlplane/internal/crypto"
	"controlplane/internal/notification"
)

func newTestTokenIssuer(t *testing.T) *notification.TokenIssuer {
	t.Helper()
	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return notification.NewTokenIssuer(crypto.NewEncryptionService(masterKey))
}

func TestToken_IssueThenValidateRoundTrips(t *testing.T) {
	issuer := newTestTokenIssuer(t)
	appID := uuid.New()

	token, err := issuer.Issue("user-42", appID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := issuer.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.UserID != "user-42" {
		t.Fatalf("UserID = %q, want user-42", claims.UserID)
	}
	if claims.ApplicationID != appID {
		t.Fatalf("ApplicationID = %v, want %v", claims.ApplicationID, appID)
	}
}

func TestToken_ValidateRejectsGarbage(t *testing.T) {
	issuer := newTestTokenIssuer(t)

	if _, err := issuer.Validate("not-a-real-token"); err == nil {
		t.Fatal("Validate accepted garbage input")
	}
}

func TestToken_ValidateRejectsTokenFromDifferentKey(t *testing.T) {
	issuer1 := newTestTokenIssuer(t)
	issuer2 := newTestTokenIssuer(t)

	token, err := issuer1.Issue("user-42", uuid.New())
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := issuer2.Validate(token); err == nil {
		t.Fatal("Validate accepted a token minted under a different key")
	}
}
