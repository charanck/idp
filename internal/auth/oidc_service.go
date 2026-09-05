package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"controlplane/internal/crypto"
	model "controlplane/internal/model/auth"
	"controlplane/internal/security"
)

// authCodeTTL is how long an issued authorization code remains exchangeable.
const authCodeTTL = 2 * time.Minute

// tokenTTL is the lifetime of every issued ID/access token. There are no
// refresh tokens in v1: a relying party that needs a fresh session simply
// re-runs the authorize redirect.
const tokenTTL = 1 * time.Hour

// ErrOIDCClientInvalid is returned by ValidateClient when client_id doesn't
// resolve to an active auth application, or redirectURI isn't one of its
// registered redirect URIs.
var ErrOIDCClientInvalid = errors.New("invalid client or redirect_uri")

// ErrOIDCUserNotAllowed is returned when the authenticated user isn't a
// member of any of the client's allowed groups.
var ErrOIDCUserNotAllowed = errors.New("user not allowed to log into this application")

// ErrOIDCCodeInvalid is returned by ExchangeCode when the authorization code
// doesn't exist, was already used, expired, or doesn't match the calling
// client/redirect_uri.
var ErrOIDCCodeInvalid = errors.New("invalid or expired authorization code")

// ErrOIDCClientAuthFailed is returned when client_id/client_secret don't
// authenticate as an active auth application.
var ErrOIDCClientAuthFailed = errors.New("client authentication failed")

// OIDCService turns the existing ServiceClient/Group/User models into a
// standard OIDC Identity Provider: other applications ("auth applications")
// redirect their users here to log in via an authorization-code flow and get
// back RS256-signed tokens. This is unrelated to OAuthService, which handles
// control-plane's own users logging in via an external IdP.
type OIDCService struct {
	signingKeys model.OIDCSigningKeyRepository
	authCodes   model.OIDCAuthorizationCodeRepository
	clients     model.ServiceClientRepository
	groups      model.GroupRepository
	users       model.UserRepository
	encryption  *crypto.EncryptionService
}

func NewOIDCService(
	signingKeys model.OIDCSigningKeyRepository,
	authCodes model.OIDCAuthorizationCodeRepository,
	clients model.ServiceClientRepository,
	groups model.GroupRepository,
	users model.UserRepository,
	encryption *crypto.EncryptionService,
) *OIDCService {
	return &OIDCService{
		signingKeys: signingKeys,
		authCodes:   authCodes,
		clients:     clients,
		groups:      groups,
		users:       users,
		encryption:  encryption,
	}
}

// EnsureSigningKey returns the singleton RS256 keypair, generating one on
// first call.
func (s *OIDCService) EnsureSigningKey(ctx context.Context) (*rsa.PrivateKey, string, error) {
	row, err := s.signingKeys.GetOrCreate(ctx, s.generateSigningKeyRow)
	if err != nil {
		return nil, "", err
	}
	privPEM, err := s.encryption.DecryptFromStorage(row.EncryptedPrivateKey)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt oidc signing key: %w", err)
	}
	privKey, err := parseRSAPrivateKeyPEM(privPEM)
	if err != nil {
		return nil, "", err
	}
	return privKey, row.Kid, nil
}

func (s *OIDCService) generateSigningKeyRow() (*model.OIDCSigningKey, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&privKey.PublicKey),
	})
	encryptedPriv, err := s.encryption.EncryptForStorage(string(privPEM))
	if err != nil {
		return nil, fmt.Errorf("encrypt oidc signing key: %w", err)
	}
	kid, err := randomURLSafe(8)
	if err != nil {
		return nil, err
	}
	return &model.OIDCSigningKey{
		Kid:                 kid,
		EncryptedPrivateKey: encryptedPriv,
		PublicKeyPEM:        string(pubPEM),
	}, nil
}

func parseRSAPrivateKeyPEM(privPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return nil, errors.New("invalid oidc signing key PEM")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func parseRSAPublicKeyPEM(pubPEM string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pubPEM))
	if block == nil {
		return nil, errors.New("invalid oidc signing key PEM")
	}
	return x509.ParsePKCS1PublicKey(block.Bytes)
}

// JWKS builds the JSON Web Key Set for /.well-known/jwks.json from the
// singleton signing key's public half.
func (s *OIDCService) JWKS(ctx context.Context) (map[string]any, error) {
	row, err := s.signingKeys.GetOrCreate(ctx, s.generateSigningKeyRow)
	if err != nil {
		return nil, err
	}
	pubKey, err := parseRSAPublicKeyPEM(row.PublicKeyPEM)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": row.Kid,
				"n":   base64.RawURLEncoding.EncodeToString(pubKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pubKey.E)).Bytes()),
			},
		},
	}, nil
}

// ValidateClient resolves clientID to an active auth application and checks
// that redirectURI exactly matches one of its registered redirect URIs.
func (s *OIDCService) ValidateClient(ctx context.Context, clientID, redirectURI string) (*model.ServiceClient, error) {
	client, err := s.clients.FindByAPIKeyIDActive(ctx, clientID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOIDCClientInvalid
	}
	if err != nil {
		return nil, err
	}
	if !client.IsAuthApplication {
		return nil, ErrOIDCClientInvalid
	}
	redirectURIs, err := s.clients.ListRedirectURIs(ctx, client.ID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(redirectURIs, redirectURI) {
		return nil, ErrOIDCClientInvalid
	}
	return client, nil
}

// UserAllowedForClient reports whether userID may log into client: true if
// the client's allowed-groups list is empty (any directory user), or the
// user belongs to at least one of those groups.
func (s *OIDCService) UserAllowedForClient(ctx context.Context, client *model.ServiceClient, userID uuid.UUID) (bool, error) {
	allowedGroupIDs, err := s.clients.ListAllowedGroupIDs(ctx, client.ID)
	if err != nil {
		return false, err
	}
	if len(allowedGroupIDs) == 0 {
		return true, nil
	}
	allowed := make(map[uuid.UUID]bool, len(allowedGroupIDs))
	for _, id := range allowedGroupIDs {
		allowed[id] = true
	}
	userGroups, err := s.groups.ListByUserID(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, g := range userGroups {
		if allowed[g.ID] {
			return true, nil
		}
	}
	return false, nil
}

// IssueAuthorizationCode creates a single-use, short-lived authorization
// code for userID to complete login to client.
func (s *OIDCService) IssueAuthorizationCode(ctx context.Context, client *model.ServiceClient, userID uuid.UUID, redirectURI, scope, nonce string) (*model.OIDCAuthorizationCode, error) {
	code, err := randomURLSafe(24)
	if err != nil {
		return nil, err
	}
	authCode := &model.OIDCAuthorizationCode{
		Code:            code,
		ServiceClientID: client.ID,
		UserID:          userID,
		RedirectURI:     redirectURI,
		Scope:           scope,
		ExpiresAt:       time.Now().UTC().Add(authCodeTTL),
	}
	if nonce != "" {
		authCode.Nonce = &nonce
	}
	if err := s.authCodes.Create(ctx, authCode); err != nil {
		return nil, err
	}
	return authCode, nil
}

// AuthenticateClientCredentials verifies clientID/clientSecret the same way
// AuthenticateServiceAPIKey verifies "<key_id>.<secret>", just with the two
// parts already split (form body or HTTP Basic auth, per OAuth2 convention).
func (s *OIDCService) AuthenticateClientCredentials(ctx context.Context, clientID, clientSecret string) (*model.ServiceClient, error) {
	client, err := s.clients.FindByAPIKeyIDActive(ctx, clientID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOIDCClientAuthFailed
	}
	if err != nil {
		return nil, err
	}
	if !client.IsAuthApplication {
		return nil, ErrOIDCClientAuthFailed
	}
	if client.APIKeyHash == "" || !security.VerifyPassword(clientSecret, client.APIKeyHash) {
		return nil, ErrOIDCClientAuthFailed
	}
	return client, nil
}

// ExchangeCode authenticates the client, consumes code (rejecting reuse,
// expiry, or a client/redirect_uri mismatch), and returns a signed ID token
// and access token for the code's user, scoped to the code's originally
// requested scope. issuer is this control-plane's own base URL (scheme +
// host), used as the tokens' "iss" claim - there's no fixed public-URL
// config, so callers derive it from the incoming request, same as
// web/oauth_login_handler.go's redirect_uri construction.
func (s *OIDCService) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI, issuer string) (idToken, accessToken string, expiresIn int, err error) {
	client, err := s.AuthenticateClientCredentials(ctx, clientID, clientSecret)
	if err != nil {
		return "", "", 0, err
	}

	authCode, err := s.authCodes.FindAndConsume(ctx, code)
	if err != nil {
		return "", "", 0, err
	}
	if authCode == nil || authCode.ServiceClientID != client.ID || authCode.RedirectURI != redirectURI {
		return "", "", 0, ErrOIDCCodeInvalid
	}

	user, err := s.users.FindActiveByID(ctx, authCode.UserID)
	if err != nil {
		return "", "", 0, err
	}

	privKey, kid, err := s.EnsureSigningKey(ctx)
	if err != nil {
		return "", "", 0, err
	}

	scopes := strings.Fields(authCode.Scope)
	hasScope := make(map[string]bool, len(scopes))
	for _, sc := range scopes {
		hasScope[sc] = true
	}

	now := time.Now().UTC()
	expiresAt := now.Add(tokenTTL)

	idClaims := jwt.MapClaims{
		"iss": issuer,
		"sub": user.ID.String(),
		"aud": clientID,
		"exp": expiresAt.Unix(),
		"iat": now.Unix(),
	}
	if authCode.Nonce != nil {
		idClaims["nonce"] = *authCode.Nonce
	}
	if hasScope["profile"] {
		idClaims["preferred_username"] = user.Username
	}
	if hasScope["email"] {
		idClaims["email"] = user.Email
	}
	if hasScope["groups"] {
		userGroups, err := s.groups.ListByUserID(ctx, user.ID)
		if err != nil {
			return "", "", 0, err
		}
		names := make([]string, len(userGroups))
		for i, g := range userGroups {
			names[i] = g.Name
		}
		idClaims["groups"] = names
	}

	accessClaims := jwt.MapClaims{
		"iss":   issuer,
		"sub":   user.ID.String(),
		"aud":   clientID,
		"exp":   expiresAt.Unix(),
		"iat":   now.Unix(),
		"scope": authCode.Scope,
	}

	idToken, err = signClaims(privKey, kid, idClaims)
	if err != nil {
		return "", "", 0, err
	}
	accessToken, err = signClaims(privKey, kid, accessClaims)
	if err != nil {
		return "", "", 0, err
	}
	return idToken, accessToken, int(tokenTTL.Seconds()), nil
}

func signClaims(privKey *rsa.PrivateKey, kid string, claims jwt.MapClaims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	return token.SignedString(privKey)
}

// ValidateAccessToken verifies tokenString's RS256 signature and expiry,
// returning its claims for the userinfo endpoint.
func (s *OIDCService) ValidateAccessToken(ctx context.Context, tokenString string) (map[string]any, error) {
	row, err := s.signingKeys.Get(ctx)
	if err != nil {
		return nil, err
	}
	pubKey, err := parseRSAPublicKeyPEM(row.PublicKeyPEM)
	if err != nil {
		return nil, err
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		return pubKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid access token")
	}
	return claims, nil
}
