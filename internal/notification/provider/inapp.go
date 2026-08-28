package provider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// InAppRecipient is the typed shape of a "channel":"inapp" notification's
// Recipient JSON. Unlike the other channels, UserID is required - an inapp
// notification only exists to be listed from that user's unread inbox, so a
// recipient without one would be unretrievable.
type InAppRecipient struct {
	UserID string `json:"user_id"`
}

// InAppContent is the typed shape of a "channel":"inapp" notification's
// Content JSON.
type InAppContent struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// InAppChannel has no external provider to call out to: a notification on
// this channel is "delivered" simply by being persisted, ready for its
// recipient to pull via the unread-inbox API. This is the opposite of SSE
// (which pushes in real time but is never persisted) - InApp is pull-based
// and always persisted.
type InAppChannel struct{}

func (InAppChannel) Validate(recipient, content []byte) error {
	var r InAppRecipient
	if err := json.Unmarshal(recipient, &r); err != nil {
		return errors.New("inapp recipient must be a JSON object")
	}
	if r.UserID == "" {
		return errors.New(`inapp recipient requires "user_id"`)
	}

	var c InAppContent
	if err := json.Unmarshal(content, &c); err != nil {
		return errors.New("inapp content must be a JSON object")
	}
	if c.Title == "" {
		return errors.New(`inapp content requires "title"`)
	}
	return nil
}

func (InAppChannel) Send(ctx context.Context, n Notification, settings Settings) (*Result, error) {
	slog.InfoContext(ctx, "storing inapp notification", "recipient", string(n.Recipient))
	return &Result{Provider: "inapp", ProviderMessageID: "inapp-" + uuid.NewString()}, nil
}
