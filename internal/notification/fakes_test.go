package notification_test

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	model "controlplane/internal/model/notification"
	"controlplane/internal/notification"
	"controlplane/internal/notification/provider"
)

type fakeNotificationRepository struct {
	mu            sync.Mutex
	notifications map[uuid.UUID]model.Notification
}

func newFakeNotificationRepository() *fakeNotificationRepository {
	return &fakeNotificationRepository{notifications: make(map[uuid.UUID]model.Notification)}
}

func (f *fakeNotificationRepository) FindByIdempotencyKey(ctx context.Context, key string) (*model.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.notifications {
		if n.IdempotencyKey != nil && *n.IdempotencyKey == key {
			cp := n
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeNotificationRepository) FindByID(ctx context.Context, id uuid.UUID) (*model.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notifications[id]
	if !ok {
		return nil, nil
	}
	return &n, nil
}

func (f *fakeNotificationRepository) List(ctx context.Context, filter model.ListNotificationsFilter) ([]model.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []model.Notification
	for _, n := range f.notifications {
		if filter.Channel != "" && n.Channel != filter.Channel {
			continue
		}
		if filter.Status != "" && n.Status != filter.Status {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeNotificationRepository) Create(ctx context.Context, n *model.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now()
	}
	f.notifications[n.ID] = *n
	return nil
}

func (f *fakeNotificationRepository) ConsumeUnreadInApp(ctx context.Context, userID string) ([]model.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []model.Notification
	for _, n := range f.notifications {
		if n.Channel != model.ChannelInApp || n.ReadAt != nil {
			continue
		}
		var recipient struct {
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(n.Recipient, &recipient); err != nil || recipient.UserID != userID {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })

	now := time.Now()
	for i := range out {
		n := f.notifications[out[i].ID]
		n.ReadAt = &now
		f.notifications[n.ID] = n
		out[i].ReadAt = &now
	}
	return out, nil
}

func (f *fakeNotificationRepository) MarkProcessing(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notifications[id]
	if !ok {
		return nil
	}
	n.Status = model.StatusProcessing
	f.notifications[id] = n
	return nil
}

func (f *fakeNotificationRepository) MarkSent(ctx context.Context, id uuid.UUID, provider, providerMessageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notifications[id]
	if !ok {
		return nil
	}
	n.Status = model.StatusSent
	n.Provider = &provider
	n.ProviderMessageID = &providerMessageID
	n.Error = nil
	f.notifications[id] = n
	return nil
}

func (f *fakeNotificationRepository) MarkRetrying(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notifications[id]
	if !ok {
		return nil
	}
	n.Status = model.StatusRetrying
	n.Attempt = attempt
	errMsg := sendErr.Error()
	n.Error = &errMsg
	f.notifications[id] = n
	return nil
}

func (f *fakeNotificationRepository) MarkFailed(ctx context.Context, id uuid.UUID, attempt int, sendErr error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notifications[id]
	if !ok {
		return nil
	}
	n.Status = model.StatusFailed
	n.Attempt = attempt
	errMsg := sendErr.Error()
	n.Error = &errMsg
	f.notifications[id] = n
	return nil
}

var _ model.NotificationRepository = (*fakeNotificationRepository)(nil)

type fakeProviderSettingRepository struct {
	mu       sync.Mutex
	settings map[string]model.ProviderSetting
}

func newFakeProviderSettingRepository() *fakeProviderSettingRepository {
	return &fakeProviderSettingRepository{settings: make(map[string]model.ProviderSetting)}
}

func (f *fakeProviderSettingRepository) FindByChannel(ctx context.Context, channel string) (*model.ProviderSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.settings[channel]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (f *fakeProviderSettingRepository) List(ctx context.Context) ([]model.ProviderSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.ProviderSetting
	for _, s := range f.settings {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}

func (f *fakeProviderSettingRepository) Create(ctx context.Context, setting *model.ProviderSetting) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if setting.ID == uuid.Nil {
		setting.ID = uuid.New()
	}
	f.settings[setting.Channel] = *setting
	return nil
}

func (f *fakeProviderSettingRepository) Update(ctx context.Context, setting *model.ProviderSetting) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings[setting.Channel] = *setting
	return nil
}

var _ model.ProviderSettingRepository = (*fakeProviderSettingRepository)(nil)

// fakeHub records PublishSent calls instead of talking to Redis, so
// Worker's "only inapp publishes" policy can be tested without miniredis.
type fakeHub struct {
	mu        sync.Mutex
	published []model.Notification
}

func (f *fakeHub) PublishSent(ctx context.Context, n *model.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, *n)
	return nil
}

func (f *fakeHub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.published)
}

var _ notification.Publisher = (*fakeHub)(nil)

// fakeChannel is a scripted provider.Channel: it returns whatever
// result/error it was constructed with, and records the Settings it was
// called with so tests can assert the worker loaded/decrypted them
// correctly.
type fakeChannel struct {
	result *provider.Result
	err    error

	mu              sync.Mutex
	lastSettings    provider.Settings
	sawSettings     bool
	invocationCount int
}

func (f *fakeChannel) Validate(recipient, content []byte) error { return nil }

func (f *fakeChannel) Send(ctx context.Context, n provider.Notification, settings provider.Settings) (*provider.Result, error) {
	f.mu.Lock()
	f.lastSettings = settings
	f.sawSettings = true
	f.invocationCount++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

var _ provider.Channel = (*fakeChannel)(nil)
