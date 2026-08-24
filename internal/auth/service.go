package auth

import (
	"context"
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
func (s *AuthService) RegisterUser(ctx context.Context, email, password, username string) (*User, error) {
	db := s.db.WithContext(ctx)

	var existing User
	err := db.Where("email = ?", email).First(&existing).Error
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
	if err := db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// AuthenticateUser authenticates a user by email and password.
func (s *AuthService) AuthenticateUser(ctx context.Context, email, password string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).Where("email = ? AND is_active = ?", email, true).First(&user).Error
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
func (s *AuthService) SetPassword(ctx context.Context, userID uuid.UUID, hashedPassword string) error {
	return s.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"password":             hashedPassword,
		"force_password_reset": false,
	}).Error
}

// GetUserByID returns an active user by ID.
func (s *AuthService) GetUserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).Where("id = ? AND is_active = ?", id, true).First(&user).Error
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
func (s *AuthService) CreateServiceClient(ctx context.Context, name string) (*ServiceClientCredentials, error) {
	db := s.db.WithContext(ctx)

	var existing ServiceClient
	err := db.Where("name = ?", name).First(&existing).Error
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
	if err := db.Create(client).Error; err != nil {
		return nil, err
	}

	return &ServiceClientCredentials{Client: client, APIKey: rawAPIKey}, nil
}

// AuthenticateServiceAPIKey authenticates a service client using the
// "<key_id>.<secret>" format.
func (s *AuthService) AuthenticateServiceAPIKey(ctx context.Context, apiKey string) (*ServiceClient, error) {
	keyID, secret, ok := strings.Cut(apiKey, ".")
	if !ok || keyID == "" || secret == "" {
		return nil, nil
	}

	var client ServiceClient
	err := s.db.WithContext(ctx).Where("is_active = ? AND api_key_id = ?", true, keyID).First(&client).Error
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

// ListUsers lists users, optionally filtered by a case-insensitive email
// substring and staff status, ordered newest-first.
func (s *AuthService) ListUsers(ctx context.Context, q string, isStaff *bool) ([]User, error) {
	query := s.db.WithContext(ctx).Order("created_at DESC")
	if q != "" {
		query = query.Where("email ILIKE ?", "%"+q+"%")
	}
	if isStaff != nil {
		query = query.Where("is_staff = ?", *isStaff)
	}
	var users []User
	if err := query.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// GetUserByIDAny returns a user by ID regardless of active status, or nil if not found.
func (s *AuthService) GetUserByIDAny(ctx context.Context, id uuid.UUID) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).First(&user, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// CreateUserAdminInput bundles CreateUserAdmin's fields.
type CreateUserAdminInput struct {
	Email    string
	Username string
	Password string
	IsActive bool
}

// CreateUserAdmin creates a new user directly (as opposed to RegisterUser's
// always-inactive self-registration flow), returning ErrAlreadyExists if the
// email is taken.
func (s *AuthService) CreateUserAdmin(ctx context.Context, in CreateUserAdminInput) (*User, error) {
	db := s.db.WithContext(ctx)

	var existing User
	err := db.Where("email = ?", in.Email).First(&existing).Error
	if err == nil {
		return nil, fmt.Errorf("user already exists: %w", ErrAlreadyExists)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	hashed, err := security.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	user := &User{
		Email:    in.Email,
		Username: in.Username,
		Password: hashed,
		IsActive: in.IsActive,
	}
	if err := db.Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// UpdateUserAdminInput bundles UpdateUserAdmin's mutable fields.
type UpdateUserAdminInput struct {
	Email    string
	Username string
	IsActive bool
	IsStaff  bool
}

// UpdateUserAdmin updates a user's profile/role fields by ID, returning nil if not found.
func (s *AuthService) UpdateUserAdmin(ctx context.Context, id uuid.UUID, in UpdateUserAdminInput) (*User, error) {
	user, err := s.GetUserByIDAny(ctx, id)
	if err != nil || user == nil {
		return user, err
	}
	user.Email = in.Email
	user.Username = in.Username
	user.IsActive = in.IsActive
	user.IsStaff = in.IsStaff
	if err := s.db.WithContext(ctx).Save(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// DeleteUser deletes a user by ID, returning nil if not found.
func (s *AuthService) DeleteUser(ctx context.Context, id uuid.UUID) (*User, error) {
	user, err := s.GetUserByIDAny(ctx, id)
	if err != nil || user == nil {
		return user, err
	}
	if err := s.db.WithContext(ctx).Delete(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

// ListServiceClients lists service clients, optionally filtered by a
// case-insensitive name substring and active status, ordered newest-first.
func (s *AuthService) ListServiceClients(ctx context.Context, q string, isActive *bool) ([]ServiceClient, error) {
	query := s.db.WithContext(ctx).Order("created_at DESC")
	if q != "" {
		query = query.Where("name ILIKE ?", "%"+q+"%")
	}
	if isActive != nil {
		query = query.Where("is_active = ?", *isActive)
	}
	var clients []ServiceClient
	if err := query.Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

// GetServiceClientByIDAny returns a service client by ID regardless of
// active status, or nil if not found.
func (s *AuthService) GetServiceClientByIDAny(ctx context.Context, id uuid.UUID) (*ServiceClient, error) {
	var client ServiceClient
	err := s.db.WithContext(ctx).First(&client, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return &client, nil
}

// ToggleServiceClient flips a service client's active state by ID, returning nil if not found.
func (s *AuthService) ToggleServiceClient(ctx context.Context, id uuid.UUID) (*ServiceClient, error) {
	client, err := s.GetServiceClientByIDAny(ctx, id)
	if err != nil || client == nil {
		return client, err
	}
	client.IsActive = !client.IsActive
	if err := s.db.WithContext(ctx).Save(client).Error; err != nil {
		return nil, err
	}
	return client, nil
}

// DeleteServiceClient deletes a service client by ID, returning nil if not found.
func (s *AuthService) DeleteServiceClient(ctx context.Context, id uuid.UUID) (*ServiceClient, error) {
	client, err := s.GetServiceClientByIDAny(ctx, id)
	if err != nil || client == nil {
		return client, err
	}
	if err := s.db.WithContext(ctx).Delete(client).Error; err != nil {
		return nil, err
	}
	return client, nil
}

// RegenerateServiceClientKey rotates a service client's encryption key by
// ID, returning nil if not found.
func (s *AuthService) RegenerateServiceClientKey(ctx context.Context, id uuid.UUID) (*ServiceClient, error) {
	client, err := s.GetServiceClientByIDAny(ctx, id)
	if err != nil || client == nil {
		return client, err
	}
	newKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	client.EncryptionKey = newKey
	if err := s.db.WithContext(ctx).Save(client).Error; err != nil {
		return nil, err
	}
	return client, nil
}

// GetServiceClientByID returns an active service client by ID.
func (s *AuthService) GetServiceClientByID(ctx context.Context, id uuid.UUID) (*ServiceClient, error) {
	var client ServiceClient
	err := s.db.WithContext(ctx).Where("id = ? AND is_active = ?", id, true).First(&client).Error
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
