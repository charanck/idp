// Package provider defines the notification delivery channel interface and
// the Email/SMS implementations (email is a real SMTP sender; SMS remains a
// skeleton pending a real integration).
package provider

import (
	"context"

	"gorm.io/datatypes"
)

// Notification is the minimal view of a notification a Channel needs to send it.
type Notification struct {
	Recipient []byte // raw jsonb bytes
	Content   []byte // raw jsonb bytes
}

// Settings is a channel's provider configuration, as loaded by the worker
// for this send: Config is the non-secret jsonb blob as stored in
// ProviderSetting.Config, Credentials is already decrypted. Both are the
// zero value if the channel was never configured - a Channel that needs
// them should treat that as a permanent (non-retryable) error. Each channel
// decodes its own typed shape out of Config/Credentials, keyed by a
// "provider" discriminator field, so adding a second provider for a channel
// (e.g. a future SendGrid alongside SMTP) never requires touching this
// struct, the DB schema, or ProviderSetting.
type Settings struct {
	Config      datatypes.JSON
	Credentials string
}

// Result describes a successful send.
type Result struct {
	Provider          string
	ProviderMessageID string
}

// SendError distinguishes retryable (Transient) from permanent failures. A
// plain (non-SendError) error from a Channel defaults to transient.
type SendError struct {
	Transient bool
	Err       error
}

func (e *SendError) Error() string { return e.Err.Error() }
func (e *SendError) Unwrap() error { return e.Err }

// Channel sends a notification through a specific provider. Validate checks
// that raw recipient/content JSON matches the channel's own typed shape
// (e.g. EmailRecipient/EmailContent, defined alongside each implementation)
// before a notification is ever created - each channel owns its own
// recipient/content schema instead of a central switch statement, so adding
// a channel means adding a Channel implementation, not editing shared
// validation code.
type Channel interface {
	Validate(recipient, content []byte) error
	Send(ctx context.Context, n Notification, settings Settings) (*Result, error)
}
