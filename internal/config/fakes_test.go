package config_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	configmodel "controlplane/internal/model/config"
)

// fakeCache is a real (if trivial) in-memory cache.Cache implementation,
// unlike cache.NoopCache which always misses - it's used by tests that need
// to assert something was actually served from cache.
type fakeCache struct {
	mu       sync.Mutex
	values   map[string]string
	versions map[string]int64
}

func newFakeCache() *fakeCache {
	return &fakeCache{values: map[string]string{}, versions: map[string]int64{}}
}

func (c *fakeCache) Get(ctx context.Context, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.values[key]
	return v, ok, nil
}

func (c *fakeCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
	return nil
}

func (c *fakeCache) GetVersion(ctx context.Context, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.versions[key]; ok {
		return v, nil
	}
	return 1, nil
}

func (c *fakeCache) BumpVersion(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.versions[key]; ok {
		c.versions[key] = v + 1
	} else {
		c.versions[key] = 2
	}
	return nil
}

// fakeApplicationRepository is an in-memory configmodel.ApplicationRepository.
type fakeApplicationRepository struct {
	mu   sync.Mutex
	apps map[uuid.UUID]configmodel.Application
}

func newFakeApplicationRepository() *fakeApplicationRepository {
	return &fakeApplicationRepository{apps: make(map[uuid.UUID]configmodel.Application)}
}

func (f *fakeApplicationRepository) List(ctx context.Context, q string, allowedIDs []uuid.UUID) ([]configmodel.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var allowed map[uuid.UUID]bool
	if len(allowedIDs) > 0 {
		allowed = make(map[uuid.UUID]bool, len(allowedIDs))
		for _, id := range allowedIDs {
			allowed[id] = true
		}
	}
	var out []configmodel.Application
	for _, a := range f.apps {
		if q != "" && !strings.Contains(strings.ToLower(a.Name), strings.ToLower(q)) {
			continue
		}
		if allowed != nil && !allowed[a.ID] {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeApplicationRepository) FindByID(ctx context.Context, id uuid.UUID) (*configmodel.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.apps[id]; ok {
		cp := a
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeApplicationRepository) FindByName(ctx context.Context, name string) (*configmodel.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.apps {
		if a.Name == name {
			cp := a
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeApplicationRepository) Create(ctx context.Context, app *configmodel.Application) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if app.ID == uuid.Nil {
		app.ID = uuid.New()
	}
	f.apps[app.ID] = *app
	return nil
}

func (f *fakeApplicationRepository) Update(ctx context.Context, app *configmodel.Application) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps[app.ID] = *app
	return nil
}

func (f *fakeApplicationRepository) Delete(ctx context.Context, app *configmodel.Application) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.apps, app.ID)
	return nil
}

func (f *fakeApplicationRepository) GetOrCreate(ctx context.Context, name string) (*configmodel.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.apps {
		if a.Name == name {
			cp := a
			return &cp, nil
		}
	}
	app := configmodel.Application{ID: uuid.New(), Name: name}
	f.apps[app.ID] = app
	return &app, nil
}

func (f *fakeApplicationRepository) ListDistinctNames(ctx context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	var names []string
	for _, a := range f.apps {
		if !seen[a.Name] {
			seen[a.Name] = true
			names = append(names, a.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// fakeEnvironmentRepository is an in-memory configmodel.EnvironmentRepository.
type fakeEnvironmentRepository struct {
	mu   sync.Mutex
	envs map[uuid.UUID]configmodel.Environment
	apps *fakeApplicationRepository // used to fill in the preloaded Application field
}

func newFakeEnvironmentRepository(apps *fakeApplicationRepository) *fakeEnvironmentRepository {
	return &fakeEnvironmentRepository{envs: make(map[uuid.UUID]configmodel.Environment), apps: apps}
}

func (f *fakeEnvironmentRepository) withApplication(env configmodel.Environment) configmodel.Environment {
	if app, err := f.apps.FindByID(context.Background(), env.ApplicationID); err == nil {
		env.Application = *app
	}
	return env
}

func (f *fakeEnvironmentRepository) List(ctx context.Context, filter configmodel.ListEnvironmentsFilter) ([]configmodel.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []configmodel.Environment
	for _, e := range f.envs {
		if filter.ApplicationID != nil && e.ApplicationID != *filter.ApplicationID {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(e.Name), strings.ToLower(filter.Query)) {
			continue
		}
		out = append(out, f.withApplication(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeEnvironmentRepository) ListByApplicationID(ctx context.Context, applicationID uuid.UUID) ([]configmodel.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []configmodel.Environment
	for _, e := range f.envs {
		if e.ApplicationID == applicationID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeEnvironmentRepository) FindByID(ctx context.Context, id uuid.UUID) (*configmodel.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.envs[id]; ok {
		cp := e
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeEnvironmentRepository) FindByIDWithApplication(ctx context.Context, id uuid.UUID) (*configmodel.Environment, error) {
	f.mu.Lock()
	e, ok := f.envs[id]
	f.mu.Unlock()
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := f.withApplication(e)
	return &cp, nil
}

func (f *fakeEnvironmentRepository) FindByApplicationAndName(ctx context.Context, applicationID uuid.UUID, name string) (*configmodel.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.envs {
		if e.ApplicationID == applicationID && e.Name == name {
			cp := e
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeEnvironmentRepository) Create(ctx context.Context, env *configmodel.Environment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if env.ID == uuid.Nil {
		env.ID = uuid.New()
	}
	f.envs[env.ID] = *env
	return nil
}

func (f *fakeEnvironmentRepository) Update(ctx context.Context, env *configmodel.Environment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envs[env.ID] = *env
	return nil
}

func (f *fakeEnvironmentRepository) Delete(ctx context.Context, env *configmodel.Environment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.envs, env.ID)
	return nil
}

// fakeConfigRepository is an in-memory configmodel.ConfigRepository.
// UpsertEntryAndRecordVersion reaches into apps/envs to get-or-create scope,
// mirroring the real repository's cross-aggregate transaction but without an
// actual DB transaction - single-goroutine tests don't need one.
type fakeConfigRepository struct {
	mu               sync.Mutex
	entries          map[uuid.UUID]configmodel.ConfigEntry
	versions         map[uuid.UUID][]configmodel.ConfigEntryVersion
	apps             *fakeApplicationRepository
	envs             *fakeEnvironmentRepository
	listByScopeCalls int
}

func newFakeConfigRepository(apps *fakeApplicationRepository, envs *fakeEnvironmentRepository) *fakeConfigRepository {
	return &fakeConfigRepository{
		entries:  make(map[uuid.UUID]configmodel.ConfigEntry),
		versions: make(map[uuid.UUID][]configmodel.ConfigEntryVersion),
		apps:     apps,
		envs:     envs,
	}
}

func (f *fakeConfigRepository) withScope(e configmodel.ConfigEntry) configmodel.ConfigEntry {
	if app, err := f.apps.FindByID(context.Background(), e.ApplicationID); err == nil {
		e.Application = *app
	}
	if env, err := f.envs.FindByID(context.Background(), e.EnvironmentID); err == nil {
		e.Environment = *env
	}
	return e
}

func (f *fakeConfigRepository) FindByScopeAndKey(ctx context.Context, applicationID, environmentID uuid.UUID, key string) (*configmodel.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.entries {
		if e.ApplicationID == applicationID && e.EnvironmentID == environmentID && e.Key == key {
			cp := e
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeConfigRepository) FindByScopeAndKeyWithScope(ctx context.Context, applicationID, environmentID uuid.UUID, key string) (*configmodel.ConfigEntry, error) {
	entry, err := f.FindByScopeAndKey(ctx, applicationID, environmentID, key)
	if err != nil {
		return nil, err
	}
	cp := f.withScope(*entry)
	return &cp, nil
}

func (f *fakeConfigRepository) ListByScope(ctx context.Context, applicationID, environmentID uuid.UUID) ([]configmodel.ConfigEntry, error) {
	f.mu.Lock()
	f.listByScopeCalls++
	var matches []configmodel.ConfigEntry
	for _, e := range f.entries {
		if e.ApplicationID == applicationID && e.EnvironmentID == environmentID {
			matches = append(matches, e)
		}
	}
	f.mu.Unlock()
	out := make([]configmodel.ConfigEntry, len(matches))
	for i, e := range matches {
		out[i] = f.withScope(e)
	}
	return out, nil
}

func (f *fakeConfigRepository) FindByID(ctx context.Context, id uuid.UUID) (*configmodel.ConfigEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.entries[id]; ok {
		cp := e
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeConfigRepository) FindByIDWithScope(ctx context.Context, id uuid.UUID) (*configmodel.ConfigEntry, error) {
	entry, err := f.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	cp := f.withScope(*entry)
	return &cp, nil
}

func (f *fakeConfigRepository) List(ctx context.Context, filter configmodel.ListConfigEntriesFilter) ([]configmodel.ConfigEntry, error) {
	f.mu.Lock()
	var matches []configmodel.ConfigEntry
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
		matches = append(matches, e)
	}
	f.mu.Unlock()
	out := make([]configmodel.ConfigEntry, len(matches))
	for i, e := range matches {
		out[i] = f.withScope(e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (f *fakeConfigRepository) Update(ctx context.Context, entry *configmodel.ConfigEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[entry.ID] = *entry
	return nil
}

func (f *fakeConfigRepository) Delete(ctx context.Context, entry *configmodel.ConfigEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, entry.ID)
	delete(f.versions, entry.ID)
	return nil
}

func (f *fakeConfigRepository) RecordVersion(ctx context.Context, entry *configmodel.ConfigEntry, action, changedBy string) (*configmodel.ConfigEntryVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recordVersionLocked(entry, action, changedBy)
}

func (f *fakeConfigRepository) recordVersionLocked(entry *configmodel.ConfigEntry, action, changedBy string) (*configmodel.ConfigEntryVersion, error) {
	existing := f.versions[entry.ID]
	versionNumber := 1
	if len(existing) > 0 {
		versionNumber = existing[len(existing)-1].Version + 1
	}
	var changedByPtr *string
	if changedBy != "" {
		changedByPtr = &changedBy
	}
	v := configmodel.ConfigEntryVersion{
		ID:            uuid.New(),
		ConfigEntryID: entry.ID,
		Value:         entry.Value,
		Type:          entry.Type,
		IsSecret:      entry.IsSecret,
		Action:        action,
		Version:       versionNumber,
		ChangedBy:     changedByPtr,
		CreatedAt:     time.Now(),
	}
	f.versions[entry.ID] = append(f.versions[entry.ID], v)
	return &v, nil
}

func (f *fakeConfigRepository) UpsertEntryAndRecordVersion(ctx context.Context, params configmodel.UpsertEntryParams) (*configmodel.ConfigEntry, string, error) {
	app, err := f.apps.FindByName(ctx, params.Service)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		app = &configmodel.Application{Name: params.Service}
		if err := f.apps.Create(ctx, app); err != nil {
			return nil, "", err
		}
	} else if err != nil {
		return nil, "", err
	}

	env, err := f.envs.FindByApplicationAndName(ctx, app.ID, params.Environment)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		env = &configmodel.Environment{ApplicationID: app.ID, Name: params.Environment}
		if err := f.envs.Create(ctx, env); err != nil {
			return nil, "", err
		}
	} else if err != nil {
		return nil, "", err
	}

	f.mu.Lock()
	var found *configmodel.ConfigEntry
	for _, e := range f.entries {
		if e.ApplicationID == app.ID && e.EnvironmentID == env.ID && e.Key == params.Key {
			cp := e
			found = &cp
			break
		}
	}
	created := found == nil
	if created {
		found = &configmodel.ConfigEntry{
			ID:            uuid.New(),
			ApplicationID: app.ID,
			EnvironmentID: env.ID,
			Key:           params.Key,
			Value:         params.EncryptedValue,
			IsSecret:      params.IsSecret,
			Type:          params.ConfigType,
		}
	} else {
		found.Value = params.EncryptedValue
		found.IsSecret = params.IsSecret
		found.Type = params.ConfigType
	}
	f.entries[found.ID] = *found

	historyAction := params.HistoryAction
	if historyAction == "" {
		if created {
			historyAction = configmodel.ActionCreate
		} else {
			historyAction = configmodel.ActionUpdate
		}
	}
	if _, err := f.recordVersionLocked(found, historyAction, params.ChangedBy); err != nil {
		f.mu.Unlock()
		return nil, "", err
	}
	f.mu.Unlock()

	return found, historyAction, nil
}

func (f *fakeConfigRepository) ListVersions(ctx context.Context, configEntryID uuid.UUID) ([]configmodel.ConfigEntryVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	versions := append([]configmodel.ConfigEntryVersion(nil), f.versions[configEntryID]...)
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version > versions[j].Version })
	return versions, nil
}

func (f *fakeConfigRepository) FindVersion(ctx context.Context, configEntryID uuid.UUID, version int) (*configmodel.ConfigEntryVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.versions[configEntryID] {
		if v.Version == version {
			cp := v
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// fakeFeatureFlagRepository is an in-memory configmodel.FeatureFlagRepository.
type fakeFeatureFlagRepository struct {
	mu                     sync.Mutex
	flags                  map[uuid.UUID]configmodel.FeatureFlag
	apps                   *fakeApplicationRepository
	envs                   *fakeEnvironmentRepository
	listActiveByScopeCalls int
}

func newFakeFeatureFlagRepository(apps *fakeApplicationRepository, envs *fakeEnvironmentRepository) *fakeFeatureFlagRepository {
	return &fakeFeatureFlagRepository{flags: make(map[uuid.UUID]configmodel.FeatureFlag), apps: apps, envs: envs}
}

func (f *fakeFeatureFlagRepository) withScope(flag configmodel.FeatureFlag) configmodel.FeatureFlag {
	if app, err := f.apps.FindByID(context.Background(), flag.ApplicationID); err == nil {
		flag.Application = *app
	}
	if env, err := f.envs.FindByID(context.Background(), flag.EnvironmentID); err == nil {
		flag.Environment = *env
	}
	return flag
}

func (f *fakeFeatureFlagRepository) FindByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*configmodel.FeatureFlag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, flag := range f.flags {
		if flag.ApplicationID == applicationID && flag.EnvironmentID == environmentID && flag.Name == name {
			cp := flag
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeFeatureFlagRepository) FindActiveByScopeAndName(ctx context.Context, applicationID, environmentID uuid.UUID, name string) (*configmodel.FeatureFlag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, flag := range f.flags {
		if flag.ApplicationID == applicationID && flag.EnvironmentID == environmentID && flag.Name == name && flag.DeletedAt == nil {
			cp := flag
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeFeatureFlagRepository) ListActiveByScope(ctx context.Context, applicationID, environmentID uuid.UUID) ([]configmodel.FeatureFlag, error) {
	f.mu.Lock()
	f.listActiveByScopeCalls++
	var matches []configmodel.FeatureFlag
	for _, flag := range f.flags {
		if flag.ApplicationID == applicationID && flag.EnvironmentID == environmentID && flag.DeletedAt == nil {
			matches = append(matches, flag)
		}
	}
	f.mu.Unlock()
	out := make([]configmodel.FeatureFlag, len(matches))
	for i, flag := range matches {
		out[i] = f.withScope(flag)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeFeatureFlagRepository) Create(ctx context.Context, flag *configmodel.FeatureFlag) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if flag.ID == uuid.Nil {
		flag.ID = uuid.New()
	}
	f.flags[flag.ID] = *flag
	return nil
}

func (f *fakeFeatureFlagRepository) Update(ctx context.Context, flag *configmodel.FeatureFlag) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flags[flag.ID] = *flag
	return nil
}

func (f *fakeFeatureFlagRepository) FindByID(ctx context.Context, id uuid.UUID) (*configmodel.FeatureFlag, error) {
	f.mu.Lock()
	flag, ok := f.flags[id]
	f.mu.Unlock()
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := f.withScope(flag)
	return &cp, nil
}

func (f *fakeFeatureFlagRepository) List(ctx context.Context, filter configmodel.ListFlagsFilter) ([]configmodel.FeatureFlag, error) {
	f.mu.Lock()
	var matches []configmodel.FeatureFlag
	for _, flag := range f.flags {
		if flag.DeletedAt != nil {
			continue
		}
		if filter.ApplicationID != nil && flag.ApplicationID != *filter.ApplicationID {
			continue
		}
		if filter.EnvironmentID != nil && flag.EnvironmentID != *filter.EnvironmentID {
			continue
		}
		if filter.IsEnabled != nil && flag.IsEnabled != *filter.IsEnabled {
			continue
		}
		matches = append(matches, flag)
	}
	f.mu.Unlock()
	out := make([]configmodel.FeatureFlag, len(matches))
	for i, flag := range matches {
		out[i] = f.withScope(flag)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
