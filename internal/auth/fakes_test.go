package auth_test

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"controlplane/internal/auth"
)

// fakeUserRepository is an in-memory auth.UserRepository, mirroring the
// real gormUserRepository's not-found semantics (raw gorm.ErrRecordNotFound,
// not a swallowed nil) so AuthService/OAuthService's error-translation logic
// is exercised the same way it is against real Postgres.
type fakeUserRepository struct {
	mu    sync.Mutex
	users map[uuid.UUID]auth.User
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: make(map[uuid.UUID]auth.User)}
}

func (f *fakeUserRepository) FindByEmail(ctx context.Context, email string) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Email == email {
			cp := u
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeUserRepository) FindActiveByEmail(ctx context.Context, email string) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Email == email && u.IsActive {
			cp := u
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		cp := u
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeUserRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok && u.IsActive {
		cp := u
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeUserRepository) Create(ctx context.Context, user *auth.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	f.users[user.ID] = *user
	return nil
}

func (f *fakeUserRepository) Update(ctx context.Context, user *auth.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[user.ID] = *user
	return nil
}

func (f *fakeUserRepository) UpdatePassword(ctx context.Context, id uuid.UUID, hashedPassword string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	u.Password = hashedPassword
	u.ForcePasswordReset = false
	f.users[id] = u
	return nil
}

func (f *fakeUserRepository) Delete(ctx context.Context, user *auth.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, user.ID)
	return nil
}

func (f *fakeUserRepository) List(ctx context.Context, q string, isStaff *bool) ([]auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []auth.User
	for _, u := range f.users {
		if q != "" && !strings.Contains(strings.ToLower(u.Email), strings.ToLower(q)) {
			continue
		}
		if isStaff != nil && u.IsStaff != *isStaff {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUserRepository) CountByUsername(ctx context.Context, username string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, u := range f.users {
		if u.Username == username {
			count++
		}
	}
	return count, nil
}

// fakeServiceClientRepository is an in-memory auth.ServiceClientRepository.
type fakeServiceClientRepository struct {
	mu      sync.Mutex
	clients map[uuid.UUID]auth.ServiceClient
}

func newFakeServiceClientRepository() *fakeServiceClientRepository {
	return &fakeServiceClientRepository{clients: make(map[uuid.UUID]auth.ServiceClient)}
}

func (f *fakeServiceClientRepository) FindByName(ctx context.Context, name string) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.clients {
		if c.Name == name {
			cp := c
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeServiceClientRepository) FindByAPIKeyIDActive(ctx context.Context, apiKeyID string) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.clients {
		if c.APIKeyID != nil && *c.APIKeyID == apiKeyID && c.IsActive {
			cp := c
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeServiceClientRepository) FindByID(ctx context.Context, id uuid.UUID) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[id]; ok {
		cp := c
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeServiceClientRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[id]; ok && c.IsActive {
		cp := c
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeServiceClientRepository) Create(ctx context.Context, client *auth.ServiceClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client.ID == uuid.Nil {
		client.ID = uuid.New()
	}
	f.clients[client.ID] = *client
	return nil
}

func (f *fakeServiceClientRepository) Update(ctx context.Context, client *auth.ServiceClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[client.ID] = *client
	return nil
}

func (f *fakeServiceClientRepository) Delete(ctx context.Context, client *auth.ServiceClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.clients, client.ID)
	return nil
}

func (f *fakeServiceClientRepository) List(ctx context.Context, q string, isActive *bool) ([]auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []auth.ServiceClient
	for _, c := range f.clients {
		if q != "" && !strings.Contains(strings.ToLower(c.Name), strings.ToLower(q)) {
			continue
		}
		if isActive != nil && c.IsActive != *isActive {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// fakeOAuthProviderRepository is an in-memory auth.OAuthProviderRepository.
type fakeOAuthProviderRepository struct {
	mu        sync.Mutex
	providers map[uuid.UUID]auth.OAuthProvider
}

func newFakeOAuthProviderRepository() *fakeOAuthProviderRepository {
	return &fakeOAuthProviderRepository{providers: make(map[uuid.UUID]auth.OAuthProvider)}
}

func (f *fakeOAuthProviderRepository) List(ctx context.Context, q string, isActive *bool) ([]auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []auth.OAuthProvider
	for _, p := range f.providers {
		if q != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(q)) {
			continue
		}
		if isActive != nil && p.IsActive != *isActive {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeOAuthProviderRepository) FindByID(ctx context.Context, id uuid.UUID) (*auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.providers[id]; ok {
		cp := p
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOAuthProviderRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.providers[id]; ok && p.IsActive {
		cp := p
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOAuthProviderRepository) Create(ctx context.Context, p *auth.OAuthProvider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.providers[p.ID] = *p
	return nil
}

func (f *fakeOAuthProviderRepository) Update(ctx context.Context, p *auth.OAuthProvider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providers[p.ID] = *p
	return nil
}

func (f *fakeOAuthProviderRepository) Delete(ctx context.Context, p *auth.OAuthProvider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.providers, p.ID)
	return nil
}

// fakeOAuthUserTokenRepository is an in-memory auth.OAuthUserTokenRepository.
type fakeOAuthUserTokenRepository struct {
	mu     sync.Mutex
	tokens map[uuid.UUID]auth.OAuthUserToken
}

func newFakeOAuthUserTokenRepository() *fakeOAuthUserTokenRepository {
	return &fakeOAuthUserTokenRepository{tokens: make(map[uuid.UUID]auth.OAuthUserToken)}
}

func (f *fakeOAuthUserTokenRepository) FindByProviderAndProviderUserID(ctx context.Context, providerID uuid.UUID, providerUserID string) (*auth.OAuthUserToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tokens {
		if t.ProviderID == providerID && t.ProviderUserID == providerUserID {
			cp := t
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOAuthUserTokenRepository) Create(ctx context.Context, t *auth.OAuthUserToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	f.tokens[t.ID] = *t
	return nil
}

func (f *fakeOAuthUserTokenRepository) Update(ctx context.Context, t *auth.OAuthUserToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[t.ID] = *t
	return nil
}
