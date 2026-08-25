package webui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"

	"controlplane/internal/activity"
	"controlplane/internal/auth"
	"controlplane/internal/config"
	"controlplane/internal/dashboard"
	"controlplane/internal/notification"
	"controlplane/internal/session"
	"controlplane/internal/webui"
)

// newSessionStore returns a session.Store backed by an in-process miniredis
// instance so handler unit tests get real csrfToken()/flashes()/session
// behavior without needing a real Redis or CP_TEST_DATABASE_URL.
func newSessionStore(t *testing.T) *session.Store {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return session.NewStore(rdb, "unit-test-secret", 0)
}

// callHandler drives fn through the same session-loading (and, if user is
// non-nil, user-loading) middleware the real router applies, then invokes fn
// directly - bypassing routing/CSRF middleware, since these tests exercise
// handler logic rather than the middleware stack (that's covered by the
// existing HTTP-level integration tests in webui_test.go).
func callHandler(t *testing.T, store *session.Store, method, target string, form url.Values, user *auth.User, fn echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	return callHandlerWithParams(t, store, method, target, nil, form, user, fn)
}

// callHandlerWithParams is callHandler plus Echo route params (c.Param(...)),
// needed since fn is invoked directly rather than through Echo's router/mux,
// which is normally what populates them from the URL path.
func callHandlerWithParams(t *testing.T, store *session.Store, method, target string, params map[string]string, form url.Values, user *auth.User, fn echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()

	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if len(params) > 0 {
		names := make([]string, 0, len(params))
		values := make([]string, 0, len(params))
		for k, v := range params {
			names = append(names, k)
			values = append(values, v)
		}
		c.SetParamNames(names...)
		c.SetParamValues(values...)
	}

	loader := fakeUserLoader{}
	if user != nil {
		loader[user.ID] = *user
	}
	authMW := webui.NewAuthMiddleware(loader)

	handler := store.Middleware()(func(c echo.Context) error {
		if user != nil {
			session.FromContext(c).SetUserID(user.ID.String())
		}
		return authMW.LoadUser()(fn)(c)
	})

	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}
	return rec
}

// fakeUserLoader implements webui.UserLoader.
type fakeUserLoader map[uuid.UUID]auth.User

func (f fakeUserLoader) GetUserByID(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	if u, ok := f[id]; ok {
		cp := u
		return &cp, nil
	}
	return nil, nil
}

// fakeActivityRecorder implements webui.ActivityRecorder, recording every
// call instead of writing to a real audit log.
type fakeActivityRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeActivityRecorder) record(kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, kind)
}

func (f *fakeActivityRecorder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeActivityRecorder) LogCreate(ctx context.Context, resource, resourceID, resourceName string, details any) {
	f.record("create:" + resource)
}
func (f *fakeActivityRecorder) LogUpdate(ctx context.Context, resource, resourceID, resourceName string, details any) {
	f.record("update:" + resource)
}
func (f *fakeActivityRecorder) LogDelete(ctx context.Context, resource, resourceID, resourceName string, details any) {
	f.record("delete:" + resource)
}
func (f *fakeActivityRecorder) LogToggle(ctx context.Context, resource, resourceID, resourceName string, details any) {
	f.record("toggle:" + resource)
}
func (f *fakeActivityRecorder) LogLogin(ctx context.Context, userEmail string, details any) {
	f.record("login:" + userEmail)
}
func (f *fakeActivityRecorder) LogLogout(ctx context.Context, userEmail string) {
	f.record("logout:" + userEmail)
}
func (f *fakeActivityRecorder) LogLoginFailed(ctx context.Context, identifier string, details any) {
	f.record("login_failed:" + identifier)
}

// fakeActivityReader implements webui.ActivityReader with static, test-set results.
type fakeActivityReader struct {
	rows      []config.Activity
	resources []string
	types     []string
}

func (f *fakeActivityReader) List(ctx context.Context, filter activity.ListFilter) ([]config.Activity, error) {
	var out []config.Activity
	for _, r := range f.rows {
		if filter.Resource != "" && r.Resource != filter.Resource {
			continue
		}
		if filter.Type != "" && r.Type != filter.Type {
			continue
		}
		if filter.UserLike != "" && (r.UserEmail == nil || !strings.Contains(strings.ToLower(*r.UserEmail), strings.ToLower(filter.UserLike))) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeActivityReader) DistinctResources(ctx context.Context) ([]string, error) {
	return f.resources, nil
}

func (f *fakeActivityReader) DistinctTypes(ctx context.Context) ([]string, error) {
	return f.types, nil
}

// fakeDashboardReader implements webui.DashboardReader.
type fakeDashboardReader struct {
	counts        dashboard.Counts
	recentConfigs []config.ConfigEntry
}

func (f *fakeDashboardReader) GetCounts(ctx context.Context) (dashboard.Counts, error) {
	return f.counts, nil
}

func (f *fakeDashboardReader) RecentConfigs(ctx context.Context, limit int) ([]config.ConfigEntry, error) {
	if limit >= 0 && limit < len(f.recentConfigs) {
		return f.recentConfigs[:limit], nil
	}
	return f.recentConfigs, nil
}

// fakeApplicationStore implements webui.ApplicationStore in-memory.
type fakeApplicationStore struct {
	mu   sync.Mutex
	apps map[uuid.UUID]config.Application
}

func newFakeApplicationStore() *fakeApplicationStore {
	return &fakeApplicationStore{apps: map[uuid.UUID]config.Application{}}
}

func (f *fakeApplicationStore) put(a config.Application) config.Application {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	f.apps[a.ID] = a
	return a
}

func (f *fakeApplicationStore) ListAllApplications(ctx context.Context, q string) ([]config.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []config.Application
	for _, a := range f.apps {
		if q != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(q)) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeApplicationStore) GetApplicationByID(ctx context.Context, id uuid.UUID) (*config.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.apps[id]; ok {
		cp := a
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeApplicationStore) CreateApplication(ctx context.Context, name string) (*config.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.apps {
		if a.Name == name {
			return nil, config.ErrAlreadyExists
		}
	}
	a := config.Application{ID: uuid.New(), Name: name}
	f.apps[a.ID] = a
	return &a, nil
}

func (f *fakeApplicationStore) UpdateApplication(ctx context.Context, id uuid.UUID, name string) (*config.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.apps[id]
	if !ok {
		return nil, nil
	}
	a.Name = name
	f.apps[id] = a
	return &a, nil
}

func (f *fakeApplicationStore) DeleteApplication(ctx context.Context, id uuid.UUID) (*config.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.apps[id]
	if !ok {
		return nil, nil
	}
	delete(f.apps, id)
	return &a, nil
}

// fakeEnvironmentStore implements webui.EnvironmentStore in-memory.
type fakeEnvironmentStore struct {
	mu   sync.Mutex
	envs map[uuid.UUID]config.Environment
}

func newFakeEnvironmentStore() *fakeEnvironmentStore {
	return &fakeEnvironmentStore{envs: map[uuid.UUID]config.Environment{}}
}

func (f *fakeEnvironmentStore) put(e config.Environment) config.Environment {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.envs[e.ID] = e
	return e
}

func (f *fakeEnvironmentStore) ListAllEnvironments(ctx context.Context, filter config.ListEnvironmentsFilter) ([]config.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []config.Environment
	for _, e := range f.envs {
		if filter.ApplicationID != nil && e.ApplicationID != *filter.ApplicationID {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(filter.Query)) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeEnvironmentStore) ListEnvironmentsByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]config.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []config.Environment
	for _, e := range f.envs {
		if e.ApplicationID == applicationID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeEnvironmentStore) GetEnvironmentByID(ctx context.Context, id uuid.UUID) (*config.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.envs[id]; ok {
		cp := e
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeEnvironmentStore) GetEnvironmentWithApplicationByID(ctx context.Context, id uuid.UUID) (*config.Environment, error) {
	return f.GetEnvironmentByID(ctx, id)
}

func (f *fakeEnvironmentStore) CreateEnvironment(ctx context.Context, applicationID uuid.UUID, name string) (*config.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.envs {
		if e.ApplicationID == applicationID && e.Name == name {
			return nil, config.ErrAlreadyExists
		}
	}
	e := config.Environment{ID: uuid.New(), ApplicationID: applicationID, Name: name}
	f.envs[e.ID] = e
	return &e, nil
}

func (f *fakeEnvironmentStore) UpdateEnvironment(ctx context.Context, id, applicationID uuid.UUID, name string) (*config.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.envs[id]
	if !ok {
		return nil, nil
	}
	e.ApplicationID = applicationID
	e.Name = name
	f.envs[id] = e
	return &e, nil
}

func (f *fakeEnvironmentStore) DeleteEnvironment(ctx context.Context, id uuid.UUID) (*config.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.envs[id]
	if !ok {
		return nil, nil
	}
	delete(f.envs, id)
	return &e, nil
}

// fakeConfigStore implements webui.ConfigStore in-memory. It intentionally
// does not reimplement ConfigService's real encryption/version-numbering
// logic (that's covered by internal/config's own unit tests) - it just gives
// handler tests a controllable double.
type fakeConfigStore struct {
	mu       sync.Mutex
	entries  map[uuid.UUID]config.ConfigEntry
	versions map[uuid.UUID][]config.ConfigEntryVersion
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{entries: map[uuid.UUID]config.ConfigEntry{}, versions: map[uuid.UUID][]config.ConfigEntryVersion{}}
}

func (f *fakeConfigStore) put(e config.ConfigEntry) config.ConfigEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.entries[e.ID] = e
	return e
}

func (f *fakeConfigStore) putVersions(id uuid.UUID, versions []config.ConfigEntryVersion) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versions[id] = versions
}

func (f *fakeConfigStore) ListAllConfigEntries(ctx context.Context, filter config.ListConfigEntriesFilter) ([]config.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []config.ConfigEntry
	for _, e := range f.entries {
		if filter.ApplicationID != nil && e.ApplicationID != *filter.ApplicationID {
			continue
		}
		if filter.EnvironmentID != nil && e.EnvironmentID != *filter.EnvironmentID {
			continue
		}
		if filter.IsSecret != nil && e.IsSecret != *filter.IsSecret {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(e.Key), strings.ToLower(filter.Query)) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeConfigStore) GetConfigByID(ctx context.Context, id uuid.UUID) (*config.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.entries[id]; ok {
		cp := e
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeConfigStore) UpsertConfig(ctx context.Context, service, environment, key, value string, opts config.UpsertOptions) (*config.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, e := range f.entries {
		if e.Key == key {
			e.Value = value
			e.IsSecret = opts.IsSecret
			if opts.ConfigType != "" {
				e.Type = opts.ConfigType
			}
			f.entries[id] = e
			return &e, nil
		}
	}
	e := config.ConfigEntry{ID: uuid.New(), Key: key, Value: value, IsSecret: opts.IsSecret, Type: opts.ConfigType}
	f.entries[e.ID] = e
	return &e, nil
}

func (f *fakeConfigStore) UpdateConfigEntry(ctx context.Context, id uuid.UUID, in config.UpdateConfigEntryInput) (*config.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return nil, nil
	}
	e.ApplicationID = in.ApplicationID
	e.EnvironmentID = in.EnvironmentID
	e.Key = in.Key
	e.Value = in.Value
	e.Type = in.ConfigType
	e.IsSecret = in.IsSecret
	f.entries[id] = e
	return &e, nil
}

func (f *fakeConfigStore) DeleteConfig(ctx context.Context, configID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := uuid.Parse(configID)
	if err != nil {
		return false, nil
	}
	if _, ok := f.entries[id]; !ok {
		return false, nil
	}
	delete(f.entries, id)
	delete(f.versions, id)
	return true, nil
}

func (f *fakeConfigStore) GetConfigHistory(ctx context.Context, configID string) ([]config.ConfigEntryVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := uuid.Parse(configID)
	if err != nil {
		return nil, nil
	}
	return f.versions[id], nil
}

func (f *fakeConfigStore) RollbackConfig(ctx context.Context, configID string, version int, changedBy string) (*config.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, err := uuid.Parse(configID)
	if err != nil {
		return nil, nil
	}
	e, ok := f.entries[id]
	if !ok {
		return nil, nil
	}
	return &e, nil
}

func (f *fakeConfigStore) DecryptConfigValueOrOriginal(entry *config.ConfigEntry) string {
	return entry.Value
}

// fakeFlagStore implements webui.FlagStore in-memory.
type fakeFlagStore struct {
	mu    sync.Mutex
	flags map[uuid.UUID]config.FeatureFlag
}

func newFakeFlagStore() *fakeFlagStore {
	return &fakeFlagStore{flags: map[uuid.UUID]config.FeatureFlag{}}
}

func (f *fakeFlagStore) put(fl config.FeatureFlag) config.FeatureFlag {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fl.ID == uuid.Nil {
		fl.ID = uuid.New()
	}
	f.flags[fl.ID] = fl
	return fl
}

func (f *fakeFlagStore) ListAllFlags(ctx context.Context, filter config.ListFlagsFilter) ([]config.FeatureFlag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []config.FeatureFlag
	for _, fl := range f.flags {
		if fl.DeletedAt != nil {
			continue
		}
		if filter.ApplicationID != nil && fl.ApplicationID != *filter.ApplicationID {
			continue
		}
		if filter.EnvironmentID != nil && fl.EnvironmentID != *filter.EnvironmentID {
			continue
		}
		if filter.IsEnabled != nil && fl.IsEnabled != *filter.IsEnabled {
			continue
		}
		out = append(out, fl)
	}
	return out, nil
}

func (f *fakeFlagStore) CreateFlag(ctx context.Context, service, name string, opts config.CreateFlagOptions) ([]config.FeatureFlag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl := config.FeatureFlag{ID: uuid.New(), Name: name, IsEnabled: opts.IsEnabled}
	if opts.Description != "" {
		fl.Description = &opts.Description
	}
	f.flags[fl.ID] = fl
	return []config.FeatureFlag{fl}, nil
}

func (f *fakeFlagStore) ToggleFlagByID(ctx context.Context, id uuid.UUID) (*config.FeatureFlag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.flags[id]
	if !ok {
		return nil, nil
	}
	fl.IsEnabled = !fl.IsEnabled
	f.flags[id] = fl
	return &fl, nil
}

func (f *fakeFlagStore) GetFlagByID(ctx context.Context, id uuid.UUID) (*config.FeatureFlag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fl, ok := f.flags[id]; ok {
		cp := fl
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeFlagStore) SoftDeleteFlagByID(ctx context.Context, id uuid.UUID) (*config.FeatureFlag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fl, ok := f.flags[id]
	if !ok {
		return nil, nil
	}
	now := time.Now()
	fl.DeletedAt = &now
	f.flags[id] = fl
	return &fl, nil
}

// fakeClientStore implements webui.ClientStore in-memory.
type fakeClientStore struct {
	mu      sync.Mutex
	clients map[uuid.UUID]auth.ServiceClient
}

func newFakeClientStore() *fakeClientStore {
	return &fakeClientStore{clients: map[uuid.UUID]auth.ServiceClient{}}
}

func (f *fakeClientStore) put(c auth.ServiceClient) auth.ServiceClient {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.clients[c.ID] = c
	return c
}

func (f *fakeClientStore) ListServiceClients(ctx context.Context, q string, isActive *bool) ([]auth.ServiceClient, error) {
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

func (f *fakeClientStore) CreateServiceClient(ctx context.Context, name string) (*auth.ServiceClientCredentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.clients {
		if c.Name == name {
			return nil, auth.ErrAlreadyExists
		}
	}
	c := auth.ServiceClient{ID: uuid.New(), Name: name, EncryptionKey: "key-1", IsActive: true}
	f.clients[c.ID] = c
	return &auth.ServiceClientCredentials{Client: &c, APIKey: "test-api-key"}, nil
}

func (f *fakeClientStore) GetServiceClientByIDAny(ctx context.Context, id uuid.UUID) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[id]; ok {
		cp := c
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeClientStore) ToggleServiceClient(ctx context.Context, id uuid.UUID) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clients[id]
	if !ok {
		return nil, nil
	}
	c.IsActive = !c.IsActive
	f.clients[id] = c
	return &c, nil
}

func (f *fakeClientStore) DeleteServiceClient(ctx context.Context, id uuid.UUID) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clients[id]
	if !ok {
		return nil, nil
	}
	delete(f.clients, id)
	return &c, nil
}

func (f *fakeClientStore) RegenerateServiceClientKey(ctx context.Context, id uuid.UUID) (*auth.ServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clients[id]
	if !ok {
		return nil, nil
	}
	c.EncryptionKey = "regenerated-key"
	f.clients[id] = c
	return &c, nil
}

// fakeUserStore implements webui.UserStore in-memory.
type fakeUserStore struct {
	mu    sync.Mutex
	users map[uuid.UUID]auth.User
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{users: map[uuid.UUID]auth.User{}}
}

func (f *fakeUserStore) put(u auth.User) auth.User {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	f.users[u.ID] = u
	return u
}

func (f *fakeUserStore) ListUsers(ctx context.Context, q string, isStaff *bool) ([]auth.User, error) {
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

func (f *fakeUserStore) CreateUserAdmin(ctx context.Context, in auth.CreateUserAdminInput) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Email == in.Email {
			return nil, auth.ErrAlreadyExists
		}
	}
	u := auth.User{ID: uuid.New(), Email: in.Email, Username: in.Username, IsActive: in.IsActive}
	f.users[u.ID] = u
	return &u, nil
}

func (f *fakeUserStore) GetUserByIDAny(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		cp := u
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeUserStore) UpdateUserAdmin(ctx context.Context, id uuid.UUID, in auth.UpdateUserAdminInput) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return nil, nil
	}
	u.Email = in.Email
	u.Username = in.Username
	u.IsActive = in.IsActive
	u.IsStaff = in.IsStaff
	f.users[id] = u
	return &u, nil
}

func (f *fakeUserStore) DeleteUser(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return nil, nil
	}
	delete(f.users, id)
	return &u, nil
}

// fakeAuthStore implements webui.AuthStore.
type fakeAuthStore struct {
	mu           sync.Mutex
	usersByEmail map[string]auth.User
	authenticate func(email, password string) (*auth.User, error)
}

func newFakeAuthStore() *fakeAuthStore {
	return &fakeAuthStore{usersByEmail: map[string]auth.User{}}
}

func (f *fakeAuthStore) put(u auth.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	f.usersByEmail[u.Email] = u
}

func (f *fakeAuthStore) AuthenticateUser(ctx context.Context, email, password string) (*auth.User, error) {
	if f.authenticate != nil {
		return f.authenticate(email, password)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.usersByEmail[email]; ok {
		cp := u
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeAuthStore) SetPassword(ctx context.Context, userID uuid.UUID, hashedPassword string) error {
	return nil
}

// fakeOAuthActiveLister implements webui.OAuthActiveLister.
type fakeOAuthActiveLister struct {
	providers []auth.OAuthProvider
}

func (f fakeOAuthActiveLister) ListActiveProviders(ctx context.Context) ([]auth.OAuthProvider, error) {
	return f.providers, nil
}

// fakeRateLimiter implements webui.RateLimiter.
type fakeRateLimiter struct {
	limited bool
	err     error
}

func (f fakeRateLimiter) IsRateLimited(ctx context.Context, key, clientIP string, limit int, window time.Duration) (bool, error) {
	return f.limited, f.err
}

// fakeOAuthFlow implements webui.OAuthFlow with per-call stub functions,
// since it drives a multi-step external redirect flow rather than CRUD.
type fakeOAuthFlow struct {
	activeProvider  *auth.OAuthProvider
	provider        *auth.OAuthProvider
	authURL         string
	state           string
	authErr         error
	token           *oauth2.Token
	exchangeErr     error
	userInfo        map[string]any
	userInfoErr     error
	user            *auth.User
	authenticateErr error
}

func (f *fakeOAuthFlow) GetActiveProviderByID(ctx context.Context, id uuid.UUID) (*auth.OAuthProvider, error) {
	return f.activeProvider, nil
}

func (f *fakeOAuthFlow) GetProviderByID(ctx context.Context, id uuid.UUID) (*auth.OAuthProvider, error) {
	return f.provider, nil
}

func (f *fakeOAuthFlow) GetAuthorizationURL(provider *auth.OAuthProvider, redirectURI string) (string, string, error) {
	return f.authURL, f.state, f.authErr
}

func (f *fakeOAuthFlow) ExchangeCodeForToken(ctx context.Context, provider *auth.OAuthProvider, code, redirectURI string) (*oauth2.Token, error) {
	return f.token, f.exchangeErr
}

func (f *fakeOAuthFlow) GetUserInfo(ctx context.Context, provider *auth.OAuthProvider, accessToken string) (map[string]any, error) {
	return f.userInfo, f.userInfoErr
}

func (f *fakeOAuthFlow) AuthenticateOrCreateUser(ctx context.Context, provider *auth.OAuthProvider, token *oauth2.Token, userInfo map[string]any) (*auth.User, *auth.OAuthUserToken, error) {
	if f.authenticateErr != nil {
		return nil, nil, f.authenticateErr
	}
	return f.user, nil, nil
}

// fakeOAuthProviderStore implements webui.OAuthProviderStore in-memory.
type fakeOAuthProviderStore struct {
	mu        sync.Mutex
	providers map[uuid.UUID]auth.OAuthProvider
}

func newFakeOAuthProviderStore() *fakeOAuthProviderStore {
	return &fakeOAuthProviderStore{providers: map[uuid.UUID]auth.OAuthProvider{}}
}

func (f *fakeOAuthProviderStore) put(p auth.OAuthProvider) auth.OAuthProvider {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.providers[p.ID] = p
	return p
}

func (f *fakeOAuthProviderStore) ListProviders(ctx context.Context, q string, isActive *bool) ([]auth.OAuthProvider, error) {
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

func (f *fakeOAuthProviderStore) GetProviderByID(ctx context.Context, id uuid.UUID) (*auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.providers[id]; ok {
		cp := p
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeOAuthProviderStore) CreateProvider(ctx context.Context, p auth.OAuthProvider) (*auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.providers[p.ID] = p
	return &p, nil
}

func (f *fakeOAuthProviderStore) UpdateProvider(ctx context.Context, p auth.OAuthProvider) (*auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.providers[p.ID]; !ok {
		return nil, nil
	}
	f.providers[p.ID] = p
	return &p, nil
}

func (f *fakeOAuthProviderStore) DeleteProvider(ctx context.Context, id uuid.UUID) (*auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.providers[id]
	if !ok {
		return nil, nil
	}
	delete(f.providers, id)
	return &p, nil
}

func (f *fakeOAuthProviderStore) ToggleProvider(ctx context.Context, id uuid.UUID) (*auth.OAuthProvider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.providers[id]
	if !ok {
		return nil, nil
	}
	p.IsActive = !p.IsActive
	f.providers[id] = p
	return &p, nil
}

// fakeProviderSettingStore implements webui.ProviderSettingStore in-memory.
type fakeProviderSettingStore struct {
	mu       sync.Mutex
	settings map[string]notification.ProviderSetting
}

func newFakeProviderSettingStore() *fakeProviderSettingStore {
	return &fakeProviderSettingStore{settings: map[string]notification.ProviderSetting{}}
}

func (f *fakeProviderSettingStore) put(s notification.ProviderSetting) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.settings[s.Channel] = s
}

func (f *fakeProviderSettingStore) Get(ctx context.Context, channel string) (*notification.ProviderSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.settings[channel]; ok {
		cp := s
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeProviderSettingStore) List(ctx context.Context) ([]notification.ProviderSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []notification.ProviderSetting
	for _, s := range f.settings {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeProviderSettingStore) Upsert(ctx context.Context, in notification.UpsertInput) (*notification.ProviderSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.settings[in.Channel]
	s.ID = uuid.New()
	s.Channel = in.Channel
	s.Config = in.Config
	s.Credentials = in.Credentials
	s.IsActive = in.IsActive
	f.settings[in.Channel] = s
	return &s, nil
}
