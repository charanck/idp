package notification_test

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"controlplane/internal/notification"
)

type fakeNotificationRepository struct {
	mu            sync.Mutex
	notifications map[uuid.UUID]notification.Notification
}

func newFakeNotificationRepository() *fakeNotificationRepository {
	return &fakeNotificationRepository{notifications: make(map[uuid.UUID]notification.Notification)}
}

func (f *fakeNotificationRepository) FindByIdempotencyKey(ctx context.Context, key string) (*notification.Notification, error) {
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

func (f *fakeNotificationRepository) FindByID(ctx context.Context, id uuid.UUID) (*notification.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.notifications[id]
	if !ok {
		return nil, nil
	}
	return &n, nil
}

func (f *fakeNotificationRepository) List(ctx context.Context, filter notification.ListNotificationsFilter) ([]notification.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []notification.Notification
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

func (f *fakeNotificationRepository) Create(ctx context.Context, n *notification.Notification) error {
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

func (f *fakeNotificationRepository) ConsumeUnreadInApp(ctx context.Context, userID string) ([]notification.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []notification.Notification
	for _, n := range f.notifications {
		if n.Channel != notification.ChannelInApp || n.ReadAt != nil {
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
	n.Status = notification.StatusProcessing
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
	n.Status = notification.StatusSent
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
	n.Status = notification.StatusRetrying
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
	n.Status = notification.StatusFailed
	n.Attempt = attempt
	errMsg := sendErr.Error()
	n.Error = &errMsg
	f.notifications[id] = n
	return nil
}

var _ notification.NotificationRepository = (*fakeNotificationRepository)(nil)

type fakeProviderSettingRepository struct {
	mu       sync.Mutex
	settings map[string]notification.ProviderSetting
}

func newFakeProviderSettingRepository() *fakeProviderSettingRepository {
	return &fakeProviderSettingRepository{settings: make(map[string]notification.ProviderSetting)}
}

func (f *fakeProviderSettingRepository) FindByChannel(ctx context.Context, channel string) (*notification.ProviderSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.settings[channel]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (f *fakeProviderSettingRepository) List(ctx context.Context) ([]notification.ProviderSetting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []notification.ProviderSetting
	for _, s := range f.settings {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Channel < out[j].Channel })
	return out, nil
}

func (f *fakeProviderSettingRepository) Create(ctx context.Context, setting *notification.ProviderSetting) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if setting.ID == uuid.Nil {
		setting.ID = uuid.New()
	}
	f.settings[setting.Channel] = *setting
	return nil
}

func (f *fakeProviderSettingRepository) Update(ctx context.Context, setting *notification.ProviderSetting) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settings[setting.Channel] = *setting
	return nil
}

var _ notification.ProviderSettingRepository = (*fakeProviderSettingRepository)(nil)
