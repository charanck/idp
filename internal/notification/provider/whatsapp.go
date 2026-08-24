package provider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// WhatsAppRecipient is the typed shape of a "channel":"whatsapp"
// notification's Recipient JSON. UserID is optional - only set for
// notifications addressed to a user who can later list them from their
// unread inbox.
type WhatsAppRecipient struct {
	UserID string `json:"user_id,omitempty"`
	Phone  string `json:"phone"`
}

// WhatsAppContent is the typed shape of a "channel":"whatsapp"
// notification's Content JSON.
type WhatsAppContent struct {
	Body string `json:"body"`
}

// WhatsAppChannel is a skeleton implementation: it logs and simulates success.
// TODO: real provider SDK integration.
type WhatsAppChannel struct{}

func (WhatsAppChannel) Validate(recipient, content []byte) error {
	var r WhatsAppRecipient
	if err := json.Unmarshal(recipient, &r); err != nil {
		return errors.New("whatsapp recipient must be a JSON object")
	}
	if r.Phone == "" {
		return errors.New(`whatsapp recipient requires "phone"`)
	}

	var c WhatsAppContent
	if err := json.Unmarshal(content, &c); err != nil {
		return errors.New("whatsapp content must be a JSON object")
	}
	if c.Body == "" {
		return errors.New(`whatsapp content requires "body"`)
	}
	return nil
}

func (WhatsAppChannel) Send(ctx context.Context, n Notification) (*Result, error) {
	slog.InfoContext(ctx, "simulating whatsapp send", "recipient", string(n.Recipient))
	return &Result{Provider: "whatsapp-sim", ProviderMessageID: "sim-" + uuid.NewString()}, nil
}
