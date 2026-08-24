package provider

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
)

// EmailRecipient is the typed shape of a "channel":"email" notification's
// Recipient JSON. UserID is optional - only set for notifications addressed
// to a user who can later list them from their unread inbox.
type EmailRecipient struct {
	UserID string `json:"user_id,omitempty"`
	Email  string `json:"email"`
}

// EmailContent is the typed shape of a "channel":"email" notification's
// Content JSON.
type EmailContent struct {
	Subject string `json:"subject"`
	Body    string `json:"body,omitempty"`
}

// EmailChannel is a skeleton implementation: it logs and simulates success.
// TODO: real provider SDK integration.
type EmailChannel struct{}

func (EmailChannel) Validate(recipient, content []byte) error {
	var r EmailRecipient
	if err := json.Unmarshal(recipient, &r); err != nil {
		return errors.New("email recipient must be a JSON object")
	}
	if r.Email == "" {
		return errors.New(`email recipient requires "email"`)
	}

	var c EmailContent
	if err := json.Unmarshal(content, &c); err != nil {
		return errors.New("email content must be a JSON object")
	}
	if c.Subject == "" {
		return errors.New(`email content requires "subject"`)
	}
	return nil
}

func (EmailChannel) Send(ctx context.Context, n Notification) (*Result, error) {
	slog.InfoContext(ctx, "simulating email send", "recipient", string(n.Recipient))
	return &Result{Provider: "email-sim", ProviderMessageID: "sim-" + uuid.NewString()}, nil
}
