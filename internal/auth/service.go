package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"controlplane/internal/crypto"
	"controlplane/internal/security"
)

var ErrAlreadyExists = errors.New("already exists")

// AuthService mirrors authentication/services.py's AuthService.
type AuthService struct {
	db *gorm.DB
}

func NewAuthService(db *gorm.DB) *AuthService {
	return &AuthService{db: db}
}

// RegisterUser registers a new user, inactive until an admin activates the account.
func (s *AuthService) RegisterUser(email, password, username string) (*User, error) {
	var existing User
	err := s.db.Where("email = ?", email).First(&existing).Error
	if err == nil {
		return nil, fmt.Errorf("user already exists: %w", ErrAlreadyExists)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if username == "" {
		suffix, genErr := randomHex(4)
		if genErr != nil {
			return nil, genErr
		}
		local, _, _ := strings.Cut(email, "@")
		username = local + suffix
	}

	hashed, err := security.HashPassword(password)
	if err != nil {
		return nil, err
	}

	user := &User{
		ID:       uuid.New(),
		Email:    email,
		Username: username,
		IsActive: false,
		Password: hashed,
	}
	if err := s.db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// AuthenticateUser authenticates a user by email and password.
func (s *AuthService) AuthenticateUser(email, password string) (*User, error) {
	var user User
	err := s.db.Where("email = ? AND is_active = ?", email, true).First(&user).Error
	if err != nil {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error, matching the Python service.
	}
	if !security.VerifyPassword(password, user.Password) {
		return nil, nil
	}
	return &user, nil
}

// SetPassword updates a user's password hash and clears any pending forced
// reset, mirroring web_ui/views.py's password_change_view.
func (s *AuthService) SetPassword(userID uuid.UUID, hashedPassword string) error {
	return s.db.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"password":             hashedPassword,
		"force_password_reset": false,
	}).Error
}

// GetUserByID returns an active user by ID.
func (s *AuthService) GetUserByID(id uuid.UUID) (*User, error) {
	var user User
	err := s.db.Where("id = ? AND is_active = ?", id, true).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

type ServiceClientCredentials struct {
	Client *ServiceClient
	APIKey string
}

// CreateServiceClient creates a new service client with an API key and encryption key.
func (s *AuthService) CreateServiceClient(name string) (*ServiceClientCredentials, error) {
	var existing ServiceClient
	err := s.db.Where("name = ?", name).First(&existing).Error
	if err == nil {
		return nil, fmt.Errorf("service client already exists: %w", ErrAlreadyExists)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	keyIDSuffix, err := randomHex(4)
	if err != nil {
		return nil, err
	}
	apiKeyID := "sk_live_" + keyIDSuffix

	apiKeySecret, err := randomURLSafe(24)
	if err != nil {
		return nil, err
	}
	rawAPIKey := apiKeyID + "." + apiKeySecret

	clientEncryptionKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}

	hashedSecret, err := security.HashPassword(apiKeySecret)
	if err != nil {
		return nil, err
	}

	client := &ServiceClient{
		ID:            uuid.New(),
		Name:          name,
		APIKeyID:      &apiKeyID,
		APIKeyHash:    hashedSecret,
		EncryptionKey: clientEncryptionKey,
		IsActive:      true,
	}
	if err := s.db.Create(client).Error; err != nil {
		return nil, err
	}

	return &ServiceClientCredentials{Client: client, APIKey: rawAPIKey}, nil
}

// AuthenticateServiceAPIKey authenticates a service client using the
// "<key_id>.<secret>" format.
func (s *AuthService) AuthenticateServiceAPIKey(apiKey string) (*ServiceClient, error) {
	keyID, secret, ok := strings.Cut(apiKey, ".")
	if !ok || keyID == "" || secret == "" {
		return nil, nil
	}

	var client ServiceClient
	err := s.db.Where("is_active = ? AND api_key_id = ?", true, keyID).First(&client).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if client.APIKeyHash == "" || !security.VerifyPassword(secret, client.APIKeyHash) {
		return nil, nil
	}
	return &client, nil
}

// GetServiceClientByID returns an active service client by ID.
func (s *AuthService) GetServiceClientByID(id uuid.UUID) (*ServiceClient, error) {
	var client ServiceClient
	err := s.db.Where("id = ? AND is_active = ?", id, true).First(&client).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// randomURLSafe generates a URL-safe random string encoding nBytes random
// bytes, matching the entropy of Python's secrets.token_urlsafe(nBytes).
func randomURLSafe(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
