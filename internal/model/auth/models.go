// Package model contains the user/service-client/OAuth-provider gorm models
// and their repository interfaces. Concrete gorm implementations live in
// internal/repository/auth; business logic lives in internal/auth.
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User is a custom, email-based login user. Some legacy columns (username,
// first_name, last_name, last_login, date_joined, is_superuser) are kept only
// because they exist in the shared "users" table already - they aren't all
// actively used by the current feature set.
type User struct {
	ID                 uuid.UUID  `gorm:"column:id;type:uuid;primaryKey"`
	Password           string     `gorm:"column:password"`
	LastLogin          *time.Time `gorm:"column:last_login"`
	IsSuperuser        bool       `gorm:"column:is_superuser"`
	Username           string     `gorm:"column:username"`
	FirstName          string     `gorm:"column:first_name"`
	LastName           string     `gorm:"column:last_name"`
	Email              string     `gorm:"column:email"`
	IsStaff            bool       `gorm:"column:is_staff"`
	IsActive           bool       `gorm:"column:is_active"`
	DateJoined         time.Time  `gorm:"column:date_joined"`
	ForcePasswordReset bool       `gorm:"column:force_password_reset"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (User) TableName() string { return "users" }

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if u.DateJoined.IsZero() {
		u.DateJoined = time.Now().UTC()
	}
	return nil
}

// ServiceClient is an S2S API-key holder with its own per-client encryption
// key. It doubles as an OIDC "auth application" (relying party) when
// IsAuthApplication is set: client_id/client_secret for that flow are the
// same APIKeyID/API key secret used for S2S config/flag reads.
type ServiceClient struct {
	ID                uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	Name              string    `gorm:"column:name"`
	APIKeyID          *string   `gorm:"column:api_key_id"`
	APIKeyHash        string    `gorm:"column:api_key_hash"`
	EncryptionKey     string    `gorm:"column:encryption_key"`
	IsActive          bool      `gorm:"column:is_active"`
	IsAuthApplication bool      `gorm:"column:is_auth_application"`
	RequireConsent    bool      `gorm:"column:require_consent"`
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (ServiceClient) TableName() string { return "service_clients" }

func (c *ServiceClient) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// OIDCSigningKey is the singleton RSA-2048 keypair (id=1) used to sign every
// ID/access token this control-plane issues as an OIDC Identity Provider.
// Generated once on first use; EncryptedPrivateKey is the PEM-encoded
// private key encrypted via crypto.EncryptionService.EncryptForStorage.
type OIDCSigningKey struct {
	ID                  int16     `gorm:"column:id;primaryKey"`
	Kid                 string    `gorm:"column:kid"`
	EncryptedPrivateKey string    `gorm:"column:encrypted_private_key"`
	PublicKeyPEM        string    `gorm:"column:public_key_pem"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (OIDCSigningKey) TableName() string { return "oidc_signing_keys" }

// OIDCAuthorizationCode is a single-use, short-lived authorization code
// issued by GET /oauth2/authorize and consumed by POST /oauth2/token.
type OIDCAuthorizationCode struct {
	Code            string    `gorm:"column:code;primaryKey"`
	ServiceClientID uuid.UUID `gorm:"column:service_client_id"`
	UserID          uuid.UUID `gorm:"column:user_id"`
	RedirectURI     string    `gorm:"column:redirect_uri"`
	Scope           string    `gorm:"column:scope"`
	Nonce           *string   `gorm:"column:nonce"`
	Used            bool      `gorm:"column:used"`
	ExpiresAt       time.Time `gorm:"column:expires_at"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (OIDCAuthorizationCode) TableName() string { return "oidc_authorization_codes" }

// OAuthProvider is an OAuth2/OIDC identity provider configuration.
type OAuthProvider struct {
	ID               uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	Name             string    `gorm:"column:name"`
	ClientID         string    `gorm:"column:client_id"`
	ClientSecret     string    `gorm:"column:client_secret"`
	AuthorizationURL string    `gorm:"column:authorization_url"`
	TokenURL         string    `gorm:"column:token_url"`
	UserinfoURL      *string   `gorm:"column:userinfo_url"`
	Scope            string    `gorm:"column:scope"`
	IsActive         bool      `gorm:"column:is_active"`
	AutoCreateUsers  bool      `gorm:"column:auto_create_users"`
	CreatedAt        time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (OAuthProvider) TableName() string { return "oauth_providers" }

func (p *OAuthProvider) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

// OAuthUserToken stores the token issued to a User by an OAuthProvider.
type OAuthUserToken struct {
	ID                uuid.UUID  `gorm:"column:id;type:uuid;primaryKey"`
	UserID            uuid.UUID  `gorm:"column:user_id"`
	ProviderID        uuid.UUID  `gorm:"column:provider_id"`
	AccessToken       string     `gorm:"column:access_token"`
	RefreshToken      *string    `gorm:"column:refresh_token"`
	TokenType         string     `gorm:"column:token_type"`
	ExpiresAt         *time.Time `gorm:"column:expires_at"`
	ProviderUserID    string     `gorm:"column:provider_user_id"`
	ProviderUserEmail *string    `gorm:"column:provider_user_email"`
	CreatedAt         time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;autoUpdateTime"`

	User     User          `gorm:"foreignKey:UserID"`
	Provider OAuthProvider `gorm:"foreignKey:ProviderID"`
}

func (OAuthUserToken) TableName() string { return "oauth_user_tokens" }

func (t *OAuthUserToken) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}
