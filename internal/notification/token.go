package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/crypto"
)

// SessionTokenTTL is deliberately short, forcing the client to re-mint a
// session periodically so the credential stays genuinely short-lived.
const SessionTokenTTL = 5 * time.Minute

const (
	tokenScope = "notifications:read"
	tokenAud   = "notifications"
)

// ErrInvalidToken is returned for any malformed, forged, or expired token.
var ErrInvalidToken = errors.New("invalid notification session token")

type tokenClaims struct {
	UserID        string `json:"user_id"`
	ApplicationID string `json:"application_id"`
	Scope         string `json:"scope"`
	Aud           string `json:"aud"`
	Exp           int64  `json:"exp"`
}

// SessionClaims is the identity a validated notification session token
// authorizes: the end user, scoped to the application whose service client
// minted the token (see TokenIssuer.Issue) - the browser-facing endpoints
// (SSE, in-app inbox) filter by both, not user ID alone, so a token minted
// for one application's user can't read another application's notifications
// for that same user ID.
type SessionClaims struct {
	UserID        string
	ApplicationID uuid.UUID
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

// Issue mints a short-lived token authorizing notification access for
// userID, scoped to applicationID (the Application the minting service
// client resolved its "service" request field to - see SessionHandler).
func (t *TokenIssuer) Issue(userID string, applicationID uuid.UUID) (string, error) {
	claims := tokenClaims{
		UserID:        userID,
		ApplicationID: applicationID.String(),
		Scope:         tokenScope,
		Aud:           tokenAud,
		Exp:           time.Now().Add(SessionTokenTTL).Unix(),
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

// Validate decrypts and checks token, returning the authorized SessionClaims.
func (t *TokenIssuer) Validate(token string) (SessionClaims, error) {
	payload, err := t.encryption.DecryptFromStorage(token)
	if err != nil {
		return SessionClaims{}, ErrInvalidToken
	}

	var claims tokenClaims
	if err := json.Unmarshal([]byte(payload), &claims); err != nil {
		return SessionClaims{}, ErrInvalidToken
	}

	if claims.Scope != tokenScope || claims.Aud != tokenAud {
		return SessionClaims{}, ErrInvalidToken
	}
	if time.Now().Unix() >= claims.Exp {
		return SessionClaims{}, ErrInvalidToken
	}
	if claims.UserID == "" {
		return SessionClaims{}, ErrInvalidToken
	}
	applicationID, err := uuid.Parse(claims.ApplicationID)
	if err != nil {
		return SessionClaims{}, ErrInvalidToken
	}
	return SessionClaims{UserID: claims.UserID, ApplicationID: applicationID}, nil
}
