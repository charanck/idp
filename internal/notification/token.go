package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"controlplane/internal/crypto"
)

// sessionTokenTTL is deliberately short, forcing the client to re-mint a
// session periodically so the credential stays genuinely short-lived.
const sessionTokenTTL = 5 * time.Minute

const (
	tokenScope = "notifications:read"
	tokenAud   = "notifications"
)

// ErrInvalidToken is returned for any malformed, forged, or expired token.
var ErrInvalidToken = errors.New("invalid notification session token")

type tokenClaims struct {
	UserID string `json:"user_id"`
	Scope  string `json:"scope"`
	Aud    string `json:"aud"`
	Exp    int64  `json:"exp"`
}

// TokenIssuer mints and validates Fernet-signed notification session tokens,
// reusing the app's existing *crypto.EncryptionService (built from
// MASTER_ENCRYPTION_KEY) rather than a separate key.
type TokenIssuer struct {
	encryption *crypto.EncryptionService
}

func NewTokenIssuer(encryption *crypto.EncryptionService) *TokenIssuer {
	return &TokenIssuer{encryption: encryption}
}

// Issue mints a short-lived token authorizing notification access for userID.
func (t *TokenIssuer) Issue(userID string) (string, error) {
	claims := tokenClaims{
		UserID: userID,
		Scope:  tokenScope,
		Aud:    tokenAud,
		Exp:    time.Now().Add(sessionTokenTTL).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal token claims: %w", err)
	}
	token, err := t.encryption.EncryptForStorage(string(payload))
	if err != nil {
		return "", fmt.Errorf("encrypt token: %w", err)
	}
	return token, nil
}

// Validate decrypts and checks token, returning the authorized user ID.
func (t *TokenIssuer) Validate(token string) (string, error) {
	payload, err := t.encryption.DecryptFromStorage(token)
	if err != nil {
		return "", ErrInvalidToken
	}

	var claims tokenClaims
	if err := json.Unmarshal([]byte(payload), &claims); err != nil {
		return "", ErrInvalidToken
	}

	if claims.Scope != tokenScope || claims.Aud != tokenAud {
		return "", ErrInvalidToken
	}
	if time.Now().Unix() >= claims.Exp {
		return "", ErrInvalidToken
	}
	if claims.UserID == "" {
		return "", ErrInvalidToken
	}
	return claims.UserID, nil
}
