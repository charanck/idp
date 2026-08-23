// Package auth contains the user/service-client/OAuth-provider models and
// services, mirroring authentication/models.py, oauth_models.py and
// services.py.
package auth

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// User mirrors authentication/models.py's User (a Django AbstractUser
// subclass). Some AbstractUser columns (username, first_name, last_name,
// last_login, date_joined, is_superuser) are kept only because they exist in
// the shared "users" table already - the Go side doesn't use groups/permissions.
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

// ServiceClient mirrors authentication/models.py's ServiceClient.
type ServiceClient struct {
	ID            uuid.UUID `gorm:"column:id;type:uuid;primaryKey"`
	Name          string    `gorm:"column:name"`
	APIKeyID      *string   `gorm:"column:api_key_id"`
	APIKeyHash    string    `gorm:"column:api_key_hash"`
	EncryptionKey string    `gorm:"column:encryption_key"`
	IsActive      bool      `gorm:"column:is_active"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (ServiceClient) TableName() string { return "service_clients" }

func (c *ServiceClient) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// OAuthProvider mirrors authentication/oauth_models.py's OAuthProvider.
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

// OAuthUserToken mirrors authentication/oauth_models.py's OAuthUserToken.
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
