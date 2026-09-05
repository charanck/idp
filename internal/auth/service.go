package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"controlplane/internal/crypto"
	model "controlplane/internal/model/auth"
	"controlplane/internal/security"
)

var ErrAlreadyExists = errors.New("already exists")

// ErrDomainNotAllowed is returned by RegisterUser when the policy's
// self-registration domain allow-list rejects the email's domain.
var ErrDomainNotAllowed = errors.New("email domain not allowed to self-register")

// builtinUserGroupName is the built-in group new users are defaulted into
// (seeded by migration 00007), mirroring today's non-staff access level.
const builtinUserGroupName = "User"

// AuthService is the user/service-client authentication and management service.
type AuthService struct {
	users    model.UserRepository
	clients  model.ServiceClientRepository
	groups   model.GroupRepository
	policies model.PolicyRepository
}

func NewAuthService(users model.UserRepository, clients model.ServiceClientRepository, groups model.GroupRepository, policies model.PolicyRepository) *AuthService {
	return &AuthService{users: users, clients: clients, groups: groups, policies: policies}
}

// assignDefaultGroup adds a newly created user to the built-in User group,
// silently skipping if that group doesn't exist (e.g. in tests that don't
// seed it) rather than failing user creation over it.
func (s *AuthService) assignDefaultGroup(ctx context.Context, userID uuid.UUID) error {
	group, err := s.defaultGroupByName(ctx, builtinUserGroupName)
	if err != nil {
		return err
	}
	if group == nil {
		return nil
	}
	return s.groups.SetUserGroups(ctx, userID, []uuid.UUID{group.ID})
}

// RegisterUser registers a new user, inactive until an admin activates the
// account, rejecting emails whose domain isn't allowed by the policy's
// self-registration domain allow-list (empty allow-list = unrestricted).
func (s *AuthService) RegisterUser(ctx context.Context, email, password, username string) (*model.User, error) {
	_, err := s.users.FindByEmail(ctx, email)
	if err == nil {
		return nil, fmt.Errorf("user already exists: %w", ErrAlreadyExists)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	policy, err := s.policies.Get(ctx)
	if err != nil {
		return nil, err
	}
	if !domainAllowed(policy.SelfRegistrationAllowedDomains, email) {
		return nil, ErrDomainNotAllowed
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

	user := &model.User{
		ID:       uuid.New(),
		Email:    email,
		Username: username,
		IsActive: false,
		Password: hashed,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}
	if err := s.assignDefaultGroup(ctx, user.ID); err != nil {
		return nil, err
	}
	return user, nil
}

// AuthenticateUser authenticates a user by email and password.
func (s *AuthService) AuthenticateUser(ctx context.Context, email, password string) (*model.User, error) {
	user, err := s.users.FindActiveByEmail(ctx, email)
	if err != nil {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error, matching the Python service.
	}
	if !security.VerifyPassword(password, user.Password) {
		return nil, nil
	}
	if security.IsLegacyHash(user.Password) {
		if newHash, err := security.HashPassword(password); err != nil {
			slog.Warn("rehash legacy password failed", "user_id", user.ID, "err", err)
		} else if err := s.users.UpdatePassword(ctx, user.ID, newHash); err != nil {
			slog.Warn("persist rehashed password failed", "user_id", user.ID, "err", err)
		}
	}
	return user, nil
}

// SetPassword updates a user's password hash and clears any pending forced
// reset, mirroring web_ui/views.py's password_change_view.
func (s *AuthService) SetPassword(ctx context.Context, userID uuid.UUID, hashedPassword string) error {
	return s.users.UpdatePassword(ctx, userID, hashedPassword)
}

// GetUserByID returns an active user by ID.
func (s *AuthService) GetUserByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	user, err := s.users.FindActiveByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return user, nil
}

type ServiceClientCredentials struct {
	Client *model.ServiceClient
	APIKey string
}

// CreateServiceClient creates a new service client with an API key and encryption key.
func (s *AuthService) CreateServiceClient(ctx context.Context, name string) (*ServiceClientCredentials, error) {
	_, err := s.clients.FindByName(ctx, name)
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

	client := &model.ServiceClient{
		ID:            uuid.New(),
		Name:          name,
		APIKeyID:      &apiKeyID,
		APIKeyHash:    hashedSecret,
		EncryptionKey: clientEncryptionKey,
		IsActive:      true,
	}
	if err := s.clients.Create(ctx, client); err != nil {
		return nil, err
	}

	return &ServiceClientCredentials{Client: client, APIKey: rawAPIKey}, nil
}

// AuthenticateServiceAPIKey authenticates a service client using the
// "<key_id>.<secret>" format.
func (s *AuthService) AuthenticateServiceAPIKey(ctx context.Context, apiKey string) (*model.ServiceClient, error) {
	keyID, secret, ok := strings.Cut(apiKey, ".")
	if !ok || keyID == "" || secret == "" {
		return nil, nil
	}

	client, err := s.clients.FindByAPIKeyIDActive(ctx, keyID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if client.APIKeyHash == "" || !security.VerifyPassword(secret, client.APIKeyHash) {
		return nil, nil
	}
	if security.IsLegacyHash(client.APIKeyHash) {
		if newHash, err := security.HashPassword(secret); err != nil {
			slog.Warn("rehash legacy api key secret failed", "client_id", client.ID, "err", err)
		} else {
			client.APIKeyHash = newHash
			if err := s.clients.Update(ctx, client); err != nil {
				slog.Warn("persist rehashed api key secret failed", "client_id", client.ID, "err", err)
			}
		}
	}
	return client, nil
}

// ListUsers lists users, optionally filtered by a case-insensitive email
// substring and staff status, ordered newest-first.
func (s *AuthService) ListUsers(ctx context.Context, q string, isStaff *bool) ([]model.User, error) {
	return s.users.List(ctx, q, isStaff)
}

// GetUserByIDAny returns a user by ID regardless of active status, or nil if not found.
func (s *AuthService) GetUserByIDAny(ctx context.Context, id uuid.UUID) (*model.User, error) {
	user, err := s.users.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return user, nil
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
func (s *AuthService) CreateUserAdmin(ctx context.Context, in CreateUserAdminInput) (*model.User, error) {
	_, err := s.users.FindByEmail(ctx, in.Email)
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

	user := &model.User{
		Email:    in.Email,
		Username: in.Username,
		Password: hashed,
		IsActive: in.IsActive,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}
	if err := s.assignDefaultGroup(ctx, user.ID); err != nil {
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
func (s *AuthService) UpdateUserAdmin(ctx context.Context, id uuid.UUID, in UpdateUserAdminInput) (*model.User, error) {
	user, err := s.GetUserByIDAny(ctx, id)
	if err != nil || user == nil {
		return user, err
	}
	user.Email = in.Email
	user.Username = in.Username
	user.IsActive = in.IsActive
	user.IsStaff = in.IsStaff
	if err := s.users.Update(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// DeleteUser deletes a user by ID, returning nil if not found.
func (s *AuthService) DeleteUser(ctx context.Context, id uuid.UUID) (*model.User, error) {
	user, err := s.GetUserByIDAny(ctx, id)
	if err != nil || user == nil {
		return user, err
	}
	if err := s.users.Delete(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// ListServiceClients lists service clients, optionally filtered by a
// case-insensitive name substring and active status, ordered newest-first.
func (s *AuthService) ListServiceClients(ctx context.Context, q string, isActive *bool) ([]model.ServiceClient, error) {
	return s.clients.List(ctx, q, isActive)
}

// GetServiceClientByIDAny returns a service client by ID regardless of
// active status, or nil if not found.
func (s *AuthService) GetServiceClientByIDAny(ctx context.Context, id uuid.UUID) (*model.ServiceClient, error) {
	client, err := s.clients.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil //nolint:nilnil // "not found" is a valid outcome, not an error.
	}
	if err != nil {
		return nil, err
	}
	return client, nil
}

// ToggleServiceClient flips a service client's active state by ID, returning nil if not found.
func (s *AuthService) ToggleServiceClient(ctx context.Context, id uuid.UUID) (*model.ServiceClient, error) {
	client, err := s.GetServiceClientByIDAny(ctx, id)
	if err != nil || client == nil {
		return client, err
	}
	client.IsActive = !client.IsActive
	if err := s.clients.Update(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

// DeleteServiceClient deletes a service client by ID, returning nil if not found.
func (s *AuthService) DeleteServiceClient(ctx context.Context, id uuid.UUID) (*model.ServiceClient, error) {
	client, err := s.GetServiceClientByIDAny(ctx, id)
	if err != nil || client == nil {
		return client, err
	}
	if err := s.clients.Delete(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

// RegenerateServiceClientKey rotates a service client's encryption key by
// ID, returning nil if not found.
func (s *AuthService) RegenerateServiceClientKey(ctx context.Context, id uuid.UUID) (*model.ServiceClient, error) {
	client, err := s.GetServiceClientByIDAny(ctx, id)
	if err != nil || client == nil {
		return client, err
	}
	newKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	client.EncryptionKey = newKey
	if err := s.clients.Update(ctx, client); err != nil {
		return nil, err
	}
	return client, nil
}

// ServiceClientApplicationIDs returns the Application IDs a service client's
// S2S config/flag reads are scoped to (empty = unrestricted).
func (s *AuthService) ServiceClientApplicationIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return s.clients.ListApplicationIDs(ctx, id)
}

// ServiceClientRedirectURIs returns a service client's registered OIDC
// redirect URIs.
func (s *AuthService) ServiceClientRedirectURIs(ctx context.Context, id uuid.UUID) ([]string, error) {
	return s.clients.ListRedirectURIs(ctx, id)
}

// ServiceClientAllowedGroupIDs returns the Groups allowed to log into a
// service client acting as an OIDC auth application (empty = any user).
func (s *AuthService) ServiceClientAllowedGroupIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return s.clients.ListAllowedGroupIDs(ctx, id)
}

// UpdateServiceClientSettingsInput is the set of fields
// UpdateServiceClientSettings can change beyond the plain name; every slice
// field replaces its join table wholesale, same as SetUserGroups/SetApplications.
type UpdateServiceClientSettingsInput struct {
	ApplicationIDs    []uuid.UUID // config/flag S2S read scope; empty = unrestricted
	IsAuthApplication bool
	RequireConsent    bool
	RedirectURIs      []string
	AllowedGroupIDs   []uuid.UUID // empty = any directory user may log in
}

// UpdateServiceClientSettings updates a service client's config/flag
// Application scope and OIDC identity-provider settings, returning nil if
// not found.
func (s *AuthService) UpdateServiceClientSettings(ctx context.Context, id uuid.UUID, in UpdateServiceClientSettingsInput) (*model.ServiceClient, error) {
	client, err := s.GetServiceClientByIDAny(ctx, id)
	if err != nil || client == nil {
		return client, err
	}
	client.IsAuthApplication = in.IsAuthApplication
	client.RequireConsent = in.RequireConsent
	if err := s.clients.Update(ctx, client); err != nil {
		return nil, err
	}
	if err := s.clients.SetApplications(ctx, client.ID, in.ApplicationIDs); err != nil {
		return nil, err
	}
	if err := s.clients.SetRedirectURIs(ctx, client.ID, in.RedirectURIs); err != nil {
		return nil, err
	}
	if err := s.clients.SetAllowedGroups(ctx, client.ID, in.AllowedGroupIDs); err != nil {
		return nil, err
	}
	return client, nil
}

// GetServiceClientByID returns an active service client by ID.
func (s *AuthService) GetServiceClientByID(ctx context.Context, id uuid.UUID) (*model.ServiceClient, error) {
	client, err := s.clients.FindActiveByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return client, nil
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
