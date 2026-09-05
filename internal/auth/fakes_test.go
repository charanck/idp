package auth_test

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authmodel "controlplane/internal/model/auth"
)

// fakeUserRepository is an in-memory authmodel.UserRepository, mirroring the
// real gormUserRepository's not-found semantics (raw gorm.ErrRecordNotFound,
// not a swallowed nil) so AuthService/OAuthService's error-translation logic
// is exercised the same way it is against real Postgres.
type fakeUserRepository struct {
	mu    sync.Mutex
	users map[uuid.UUID]authmodel.User
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: make(map[uuid.UUID]authmodel.User)}
}

func (f *fakeUserRepository) FindByEmail(ctx context.Context, email string) (*authmodel.User, error) {
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

func (f *fakeUserRepository) FindActiveByEmail(ctx context.Context, email string) (*authmodel.User, error) {
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

func (f *fakeUserRepository) FindByID(ctx context.Context, id uuid.UUID) (*authmodel.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		cp := u
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeUserRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*authmodel.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok && u.IsActive {
		cp := u
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeUserRepository) Create(ctx context.Context, user *authmodel.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	f.users[user.ID] = *user
	return nil
}

func (f *fakeUserRepository) Update(ctx context.Context, user *authmodel.User) error {
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

func (f *fakeUserRepository) Delete(ctx context.Context, user *authmodel.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, user.ID)
	return nil
}

func (f *fakeUserRepository) List(ctx context.Context, q string, isStaff *bool) ([]authmodel.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []authmodel.User
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

// fakeServiceClientRepository is an in-memory authmodel.ServiceClientRepository.
type fakeServiceClientRepository struct {
	mu            sync.Mutex
	clients       map[uuid.UUID]authmodel.ServiceClient
	clientApps    map[uuid.UUID]map[uuid.UUID]struct{} // clientID -> applicationIDs
	redirectURIs  map[uuid.UUID][]string               // clientID -> redirect URIs
	allowedGroups map[uuid.UUID]map[uuid.UUID]struct{} // clientID -> groupIDs
}

func newFakeServiceClientRepository() *fakeServiceClientRepository {
	return &fakeServiceClientRepository{
		clients:       make(map[uuid.UUID]authmodel.ServiceClient),
		clientApps:    make(map[uuid.UUID]map[uuid.UUID]struct{}),
		redirectURIs:  make(map[uuid.UUID][]string),
		allowedGroups: make(map[uuid.UUID]map[uuid.UUID]struct{}),
	}
}

func (f *fakeServiceClientRepository) FindByName(ctx context.Context, name string) (*authmodel.ServiceClient, error) {
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

func (f *fakeServiceClientRepository) FindByAPIKeyIDActive(ctx context.Context, apiKeyID string) (*authmodel.ServiceClient, error) {
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

func (f *fakeServiceClientRepository) FindByID(ctx context.Context, id uuid.UUID) (*authmodel.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[id]; ok {
		cp := c
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeServiceClientRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*authmodel.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[id]; ok && c.IsActive {
		cp := c
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeServiceClientRepository) Create(ctx context.Context, client *authmodel.ServiceClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if client.ID == uuid.Nil {
		client.ID = uuid.New()
	}
	f.clients[client.ID] = *client
	return nil
}

func (f *fakeServiceClientRepository) Update(ctx context.Context, client *authmodel.ServiceClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[client.ID] = *client
	return nil
}

func (f *fakeServiceClientRepository) Delete(ctx context.Context, client *authmodel.ServiceClient) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.clients, client.ID)
	return nil
}

func (f *fakeServiceClientRepository) List(ctx context.Context, q string, isActive *bool) ([]authmodel.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []authmodel.ServiceClient
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

func (f *fakeServiceClientRepository) ListApplicationIDs(ctx context.Context, clientID uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uuid.UUID
	for id := range f.clientApps[clientID] {
		out = append(out, id)
	}
	return out, nil
}

func (f *fakeServiceClientRepository) SetApplications(ctx context.Context, clientID uuid.UUID, applicationIDs []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := make(map[uuid.UUID]struct{}, len(applicationIDs))
	for _, id := range applicationIDs {
		set[id] = struct{}{}
	}
	f.clientApps[clientID] = set
	return nil
}

func (f *fakeServiceClientRepository) ListRedirectURIs(ctx context.Context, clientID uuid.UUID) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.redirectURIs[clientID], nil
}

func (f *fakeServiceClientRepository) SetRedirectURIs(ctx context.Context, clientID uuid.UUID, redirectURIs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.redirectURIs[clientID] = redirectURIs
	return nil
}

func (f *fakeServiceClientRepository) ListAllowedGroupIDs(ctx context.Context, clientID uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uuid.UUID
	for id := range f.allowedGroups[clientID] {
		out = append(out, id)
	}
	return out, nil
}

func (f *fakeServiceClientRepository) SetAllowedGroups(ctx context.Context, clientID uuid.UUID, groupIDs []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := make(map[uuid.UUID]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		set[id] = struct{}{}
	}
	f.allowedGroups[clientID] = set
	return nil
}

// fakeOAuthProviderRepository is an in-memory authmodel.OAuthProviderRepository.
type fakeOAuthProviderRepository struct {
	mu        sync.Mutex
	providers map[uuid.UUID]authmodel.OAuthProvider
}

func newFakeOAuthProviderRepository() *fakeOAuthProviderRepository {
	return &fakeOAuthProviderRepository{providers: make(map[uuid.UUID]authmodel.OAuthProvider)}
}

func (f *fakeOAuthProviderRepository) List(ctx context.Context, q string, isActive *bool) ([]authmodel.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []authmodel.OAuthProvider
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

func (f *fakeOAuthProviderRepository) FindByID(ctx context.Context, id uuid.UUID) (*authmodel.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.providers[id]; ok {
		cp := p
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOAuthProviderRepository) FindActiveByID(ctx context.Context, id uuid.UUID) (*authmodel.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.providers[id]; ok && p.IsActive {
		cp := p
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeOAuthProviderRepository) Create(ctx context.Context, p *authmodel.OAuthProvider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.providers[p.ID] = *p
	return nil
}

func (f *fakeOAuthProviderRepository) Update(ctx context.Context, p *authmodel.OAuthProvider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.providers[p.ID] = *p
	return nil
}

func (f *fakeOAuthProviderRepository) Delete(ctx context.Context, p *authmodel.OAuthProvider) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.providers, p.ID)
	return nil
}

// fakeOAuthUserTokenRepository is an in-memory authmodel.OAuthUserTokenRepository.
type fakeOAuthUserTokenRepository struct {
	mu     sync.Mutex
	tokens map[uuid.UUID]authmodel.OAuthUserToken
}

func newFakeOAuthUserTokenRepository() *fakeOAuthUserTokenRepository {
	return &fakeOAuthUserTokenRepository{tokens: make(map[uuid.UUID]authmodel.OAuthUserToken)}
}

func (f *fakeOAuthUserTokenRepository) FindByProviderAndProviderUserID(ctx context.Context, providerID uuid.UUID, providerUserID string) (*authmodel.OAuthUserToken, error) {
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

func (f *fakeOAuthUserTokenRepository) Create(ctx context.Context, t *authmodel.OAuthUserToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	f.tokens[t.ID] = *t
	return nil
}

func (f *fakeOAuthUserTokenRepository) Update(ctx context.Context, t *authmodel.OAuthUserToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[t.ID] = *t
	return nil
}

// fakeGroupRepository is an in-memory authmodel.GroupRepository.
type fakeGroupRepository struct {
	mu           sync.Mutex
	groups       map[uuid.UUID]authmodel.Group
	userGroups   map[uuid.UUID]map[uuid.UUID]struct{} // userID -> groupIDs
	groupApps    map[uuid.UUID]map[uuid.UUID]struct{} // groupID -> applicationIDs
}

func newFakeGroupRepository() *fakeGroupRepository {
	return &fakeGroupRepository{
		groups:     make(map[uuid.UUID]authmodel.Group),
		userGroups: make(map[uuid.UUID]map[uuid.UUID]struct{}),
		groupApps:  make(map[uuid.UUID]map[uuid.UUID]struct{}),
	}
}

func (f *fakeGroupRepository) List(ctx context.Context, q string) ([]authmodel.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []authmodel.Group
	for _, g := range f.groups {
		if q != "" && !strings.Contains(strings.ToLower(g.Name), strings.ToLower(q)) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (f *fakeGroupRepository) FindByID(ctx context.Context, id uuid.UUID) (*authmodel.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g, ok := f.groups[id]; ok {
		cp := g
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeGroupRepository) Create(ctx context.Context, group *authmodel.Group) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if group.ID == uuid.Nil {
		group.ID = uuid.New()
	}
	f.groups[group.ID] = *group
	return nil
}

func (f *fakeGroupRepository) Update(ctx context.Context, group *authmodel.Group) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups[group.ID] = *group
	return nil
}

func (f *fakeGroupRepository) Delete(ctx context.Context, group *authmodel.Group) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.groups, group.ID)
	return nil
}

func (f *fakeGroupRepository) ListApplicationIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uuid.UUID
	for id := range f.groupApps[groupID] {
		out = append(out, id)
	}
	return out, nil
}

func (f *fakeGroupRepository) SetApplications(ctx context.Context, groupID uuid.UUID, applicationIDs []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := make(map[uuid.UUID]struct{}, len(applicationIDs))
	for _, id := range applicationIDs {
		set[id] = struct{}{}
	}
	f.groupApps[groupID] = set
	return nil
}

func (f *fakeGroupRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]authmodel.Group, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []authmodel.Group
	for groupID := range f.userGroups[userID] {
		if g, ok := f.groups[groupID]; ok {
			out = append(out, g)
		}
	}
	return out, nil
}

func (f *fakeGroupRepository) SetUserGroups(ctx context.Context, userID uuid.UUID, groupIDs []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := make(map[uuid.UUID]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		set[id] = struct{}{}
	}
	f.userGroups[userID] = set
	return nil
}

// fakePolicyRepository is an in-memory authmodel.PolicyRepository, mirroring
// migration 00007's singleton policies row (id=1, unrestricted by default).
type fakePolicyRepository struct {
	mu     sync.Mutex
	policy authmodel.Policy
}

func newFakePolicyRepository() *fakePolicyRepository {
	return &fakePolicyRepository{policy: authmodel.Policy{ID: 1}}
}

func (f *fakePolicyRepository) Get(ctx context.Context) (*authmodel.Policy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := f.policy
	return &cp, nil
}

func (f *fakePolicyRepository) Update(ctx context.Context, policy *authmodel.Policy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	policy.ID = 1
	f.policy = *policy
	return nil
}

// fakeOIDCSigningKeyRepository is an in-memory authmodel.OIDCSigningKeyRepository.
type fakeOIDCSigningKeyRepository struct {
	mu  sync.Mutex
	key *authmodel.OIDCSigningKey
}

func newFakeOIDCSigningKeyRepository() *fakeOIDCSigningKeyRepository {
	return &fakeOIDCSigningKeyRepository{}
}

func (f *fakeOIDCSigningKeyRepository) Get(ctx context.Context) (*authmodel.OIDCSigningKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.key == nil {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *f.key
	return &cp, nil
}

func (f *fakeOIDCSigningKeyRepository) GetOrCreate(ctx context.Context, generate func() (*authmodel.OIDCSigningKey, error)) (*authmodel.OIDCSigningKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.key != nil {
		cp := *f.key
		return &cp, nil
	}
	generated, err := generate()
	if err != nil {
		return nil, err
	}
	generated.ID = 1
	f.key = generated
	cp := *f.key
	return &cp, nil
}

// fakeOIDCAuthorizationCodeRepository is an in-memory
// authmodel.OIDCAuthorizationCodeRepository.
type fakeOIDCAuthorizationCodeRepository struct {
	mu    sync.Mutex
	codes map[string]authmodel.OIDCAuthorizationCode
}

func newFakeOIDCAuthorizationCodeRepository() *fakeOIDCAuthorizationCodeRepository {
	return &fakeOIDCAuthorizationCodeRepository{codes: make(map[string]authmodel.OIDCAuthorizationCode)}
}

func (f *fakeOIDCAuthorizationCodeRepository) Create(ctx context.Context, code *authmodel.OIDCAuthorizationCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[code.Code] = *code
	return nil
}

func (f *fakeOIDCAuthorizationCodeRepository) FindAndConsume(ctx context.Context, code string) (*authmodel.OIDCAuthorizationCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	authCode, ok := f.codes[code]
	if !ok || authCode.Used || time.Now().UTC().After(authCode.ExpiresAt) {
		return nil, nil
	}
	authCode.Used = true
	f.codes[code] = authCode
	cp := authCode
	return &cp, nil
}
