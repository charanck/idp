package provider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// SMSRecipient is the typed shape of a "channel":"sms" notification's
// Recipient JSON. UserID is optional - only set for notifications addressed
// to a user who can later list them from their unread inbox.
type SMSRecipient struct {
	UserID string `json:"user_id,omitempty"`
	Phone  string `json:"phone"`
}

// SMSContent is the typed shape of a "channel":"sms" notification's Content JSON.
type SMSContent struct {
	Body string `json:"body"`
}

// SMSChannel is a skeleton implementation: it logs and simulates success.
// TODO: real provider SDK integration.
type SMSChannel struct{}

func (SMSChannel) Validate(recipient, content []byte) error {
	var r SMSRecipient
	if err := json.Unmarshal(recipient, &r); err != nil {
		return errors.New("sms recipient must be a JSON object")
	}
	if r.Phone == "" {
		return errors.New(`sms recipient requires "phone"`)
	}

	var c SMSContent
	if err := json.Unmarshal(content, &c); err != nil {
		return errors.New("sms content must be a JSON object")
	}
	if c.Body == "" {
		return errors.New(`sms content requires "body"`)
	}
	return nil
}

func (SMSChannel) Send(ctx context.Context, n Notification, settings Settings) (*Result, error) {
	slog.InfoContext(ctx, "simulating sms send", "recipient", string(n.Recipient))
	return &Result{Provider: "sms-sim", ProviderMessageID: "sim-" + uuid.NewString()}, nil
}
