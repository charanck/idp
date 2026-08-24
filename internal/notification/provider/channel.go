// Package provider defines the notification delivery channel interface and
// skeleton (no real provider integration) Email/SMS/WhatsApp implementations.
package provider

import "context"

// Notification is the minimal view of a notification a Channel needs to send it.
type Notification struct {
	Recipient []byte // raw jsonb bytes
	Content   []byte // raw jsonb bytes
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
	Send(ctx context.Context, n Notification) (*Result, error)
}
