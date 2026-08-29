package notification

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/crypto"
)

// TestToken_ValidateRejectsExpiredToken is a white-box test (same package as
// tokenClaims) since TTL isn't otherwise injectable from outside.
func TestToken_ValidateRejectsExpiredToken(t *testing.T) {
	masterKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	enc := crypto.NewEncryptionService(masterKey)
	issuer := NewTokenIssuer(enc)

	claims := tokenClaims{
		UserID:        "user-42",
		ApplicationID: uuid.New().String(),
		Scope:         tokenScope,
		Aud:           tokenAud,
		Exp:           time.Now().Add(-time.Minute).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	token, err := enc.EncryptForStorage(string(payload))
	if err != nil {
		t.Fatalf("EncryptForStorage: %v", err)
	}

	if _, err := issuer.Validate(token); err == nil {
		t.Fatal("Validate accepted an expired token")
	}
}
