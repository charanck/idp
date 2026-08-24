// Package notification implements the S2S notification API: creating
// notifications and delivering them asynchronously over skeleton Email/SMS/
// WhatsApp/InApp channels via asynq, a fire-and-forget SSE gateway for
// real-time delivery notices, and (for the InApp channel only) a
// bearer-token endpoint for a recipient to list and mark read their unread
// notifications. This package deliberately does not import internal/auth -
// see Authenticator in middleware.go.
package notification

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	ChannelEmail    = "email"
	ChannelSMS      = "sms"
	ChannelWhatsApp = "whatsapp"
	// ChannelInApp is the only channel with a pull-based unread inbox
	// (ConsumeUnreadInAppForUser) - see provider.InAppChannel.
	ChannelInApp = "inapp"
)

const (
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusRetrying   = "retrying"
	StatusSent       = "sent"
	StatusFailed     = "failed"
)

// Notification mirrors a single send request and its delivery state.
// recipient/content are genuinely structured, non-secret data stored as
// native jsonb.
type Notification struct {
	ID                uuid.UUID      `gorm:"column:id;type:uuid;primaryKey"`
	Channel           string         `gorm:"column:channel"`
	Recipient         datatypes.JSON `gorm:"column:recipient"`
	Content           datatypes.JSON `gorm:"column:content"`
	Status            string         `gorm:"column:status"`
	Provider          *string        `gorm:"column:provider"`
	ProviderMessageID *string        `gorm:"column:provider_message_id"`
	Attempt           int            `gorm:"column:attempt"`
	IdempotencyKey    *string        `gorm:"column:idempotency_key"`
	Error             *string        `gorm:"column:error"`
	ReadAt            *time.Time     `gorm:"column:read_at"`
	CreatedAt         time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time      `gorm:"column:updated_at;autoUpdateTime"`
}

func (Notification) TableName() string { return "notifications" }

func (n *Notification) BeforeCreate(tx *gorm.DB) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	return nil
}

// ProviderSetting holds per-channel provider configuration, with credentials
// stored as opaque Fernet ciphertext (mirrors ConfigEntry's secret handling)
// - never returned decrypted from a handler.
type ProviderSetting struct {
	ID          uuid.UUID      `gorm:"column:id;type:uuid;primaryKey"`
	Channel     string         `gorm:"column:channel"`
	Config      datatypes.JSON `gorm:"column:config"`
	Credentials string         `gorm:"column:credentials"`
	IsActive    bool           `gorm:"column:is_active"`
	CreatedAt   time.Time      `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;autoUpdateTime"`
}

func (ProviderSetting) TableName() string { return "notification_provider_settings" }

func (p *ProviderSetting) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
