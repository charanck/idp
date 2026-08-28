package provider

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"

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

// EmailProvider identifies which concrete email provider a channel's
// Settings.Config belongs to - the discriminator a future second provider
// (e.g. an HTTP API-based sender) would switch on instead of adding a new
// Channel implementation.
type EmailProvider string

// EmailProviderSMTP is the only email provider implemented today: a
// generic SMTP relay (works with Mailhog/Mailpit locally, or the SMTP
// endpoint of SES/SendGrid/Mailgun/etc. in production).
const EmailProviderSMTP EmailProvider = "smtp"

// EmailTLSMode controls how EmailChannel secures its connection to the SMTP
// server.
type EmailTLSMode string

const (
	// EmailTLSNone sends over a plaintext connection - only appropriate for
	// a local dev SMTP catcher or a relay reachable solely over a trusted
	// network.
	EmailTLSNone EmailTLSMode = "none"
	// EmailTLSStartTLS upgrades a plaintext connection via STARTTLS once
	// connected - the common case for port 587.
	EmailTLSStartTLS EmailTLSMode = "starttls"
	// EmailTLSImplicit connects over TLS from the start - the common case
	// for port 465.
	EmailTLSImplicit EmailTLSMode = "tls"
)

// EmailSMTPConfig is EmailChannel's non-secret Settings.Config shape when
// Provider is EmailProviderSMTP - the only provider today. A future second
// provider adds its own struct and a case in EmailChannel.Send's provider
// switch, without touching this one.
type EmailSMTPConfig struct {
	Provider EmailProvider `json:"provider"`
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	From     string        `json:"from"`
	FromName string        `json:"from_name,omitempty"`
	TLSMode  EmailTLSMode  `json:"tls_mode"`
}

// EmailSMTPCredentials is EmailChannel's secret Settings.Credentials shape
// for EmailProviderSMTP: JSON-encoded, then Fernet-encrypted as a whole
// into ProviderSetting.Credentials. Both fields are optional - a relay that
// doesn't require auth leaves both blank.
type EmailSMTPCredentials struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// smtpDialTimeout bounds connection setup when the send's context carries
// no deadline of its own.
const smtpDialTimeout = 30 * time.Second

// EmailChannel sends over a generic SMTP relay (EmailProviderSMTP). It has
// no state of its own - all per-send configuration comes from the Settings
// the worker loads from ProviderSetting for the "email" channel.
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

func (EmailChannel) Send(ctx context.Context, n Notification, settings Settings) (*Result, error) {
	var r EmailRecipient
	if err := json.Unmarshal(n.Recipient, &r); err != nil {
		return nil, &SendError{Err: fmt.Errorf("decode email recipient: %w", err)}
	}
	var c EmailContent
	if err := json.Unmarshal(n.Content, &c); err != nil {
		return nil, &SendError{Err: fmt.Errorf("decode email content: %w", err)}
	}

	if len(settings.Config) == 0 {
		return nil, &SendError{Err: errors.New("email provider not configured")}
	}

	var probe struct {
		Provider EmailProvider `json:"provider"`
	}
	if err := json.Unmarshal(settings.Config, &probe); err != nil {
		return nil, &SendError{Err: fmt.Errorf("decode email provider config: %w", err)}
	}

	switch probe.Provider {
	case "", EmailProviderSMTP:
		return sendSMTP(ctx, r, c, settings)
	default:
		return nil, &SendError{Err: fmt.Errorf("unsupported email provider %q", probe.Provider)}
	}
}

func sendSMTP(ctx context.Context, r EmailRecipient, c EmailContent, settings Settings) (*Result, error) {
	var cfg EmailSMTPConfig
	if err := json.Unmarshal(settings.Config, &cfg); err != nil {
		return nil, &SendError{Err: fmt.Errorf("decode smtp config: %w", err)}
	}
	if cfg.Host == "" || cfg.Port == 0 || cfg.From == "" {
		return nil, &SendError{Err: errors.New("smtp config requires host, port, and from")}
	}
	switch cfg.TLSMode {
	case "", EmailTLSNone, EmailTLSStartTLS, EmailTLSImplicit:
	default:
		return nil, &SendError{Err: fmt.Errorf("smtp config has unsupported tls_mode %q", cfg.TLSMode)}
	}

	var creds EmailSMTPCredentials
	if settings.Credentials != "" {
		if err := json.Unmarshal([]byte(settings.Credentials), &creds); err != nil {
			return nil, &SendError{Err: fmt.Errorf("decode smtp credentials: %w", err)}
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := (&net.Dialer{Timeout: smtpDialTimeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &SendError{Transient: true, Err: fmt.Errorf("dial smtp server: %w", err)}
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(smtpDialTimeout))
	}

	if cfg.TLSMode == EmailTLSImplicit {
		conn = tls.Client(conn, &tls.Config{ServerName: cfg.Host})
	}

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return nil, &SendError{Transient: true, Err: fmt.Errorf("smtp handshake: %w", err)}
	}
	defer func() {
		if err := client.Quit(); err != nil {
			slog.Debug("smtp quit failed", "err", err)
		}
	}()

	if cfg.TLSMode == EmailTLSStartTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return nil, &SendError{Err: errors.New("smtp server does not support STARTTLS")}
		}
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return nil, &SendError{Transient: true, Err: fmt.Errorf("starttls: %w", err)}
		}
	}

	if creds.Username != "" {
		auth := smtp.PlainAuth("", creds.Username, creds.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return nil, &SendError{Err: fmt.Errorf("smtp auth: %w", err)}
		}
	}

	if err := client.Mail(cfg.From); err != nil {
		return nil, &SendError{Transient: true, Err: fmt.Errorf("MAIL FROM: %w", err)}
	}
	// Most RCPT rejections are permanent (e.g. "no such mailbox") - treated
	// as non-retryable rather than probing the SMTP status code.
	if err := client.Rcpt(r.Email); err != nil {
		return nil, &SendError{Err: fmt.Errorf("RCPT TO: %w", err)}
	}

	w, err := client.Data()
	if err != nil {
		return nil, &SendError{Transient: true, Err: fmt.Errorf("DATA: %w", err)}
	}
	if _, err := w.Write(buildEmailMessage(cfg, r, c)); err != nil {
		return nil, &SendError{Transient: true, Err: fmt.Errorf("write message: %w", err)}
	}
	if err := w.Close(); err != nil {
		return nil, &SendError{Transient: true, Err: fmt.Errorf("close data: %w", err)}
	}

	return &Result{Provider: "smtp", ProviderMessageID: "smtp-" + uuid.NewString()}, nil
}

// sanitizeEmailHeader strips CR/LF from a value bound for an SMTP/MIME
// header - r.Email/c.Subject/cfg.From*/etc. all originate from an external
// S2S caller (or admin-entered provider config), so without this a crafted
// value could inject extra headers or SMTP commands into the message.
func sanitizeEmailHeader(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// buildEmailMessage renders a minimal RFC 5322 message: headers plus a
// plain-text body. Content is only ever text/plain today - EmailContent has
// no separate HTML field.
func buildEmailMessage(cfg EmailSMTPConfig, r EmailRecipient, c EmailContent) []byte {
	from := sanitizeEmailHeader(cfg.From)
	if cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", sanitizeEmailHeader(cfg.FromName), from)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", sanitizeEmailHeader(r.Email))
	fmt.Fprintf(&b, "Subject: %s\r\n", sanitizeEmailHeader(c.Subject))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	b.WriteString(c.Body)
	return []byte(b.String())
}
