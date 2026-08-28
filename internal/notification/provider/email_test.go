package provider_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"controlplane/internal/notification/provider"
)

func TestEmailChannel_ValidateRequiresEmailAndSubject(t *testing.T) {
	var ch provider.EmailChannel
	if err := ch.Validate([]byte(`{}`), []byte(`{"subject":"hi"}`)); err == nil {
		t.Fatal("expected error for missing email")
	}
	if err := ch.Validate([]byte(`{"email":"a@example.com"}`), []byte(`{}`)); err == nil {
		t.Fatal("expected error for missing subject")
	}
	if err := ch.Validate([]byte(`{"email":"a@example.com"}`), []byte(`{"subject":"hi"}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmailChannel_SendFailsPermanentlyWhenNotConfigured(t *testing.T) {
	var ch provider.EmailChannel
	_, err := ch.Send(context.Background(),
		provider.Notification{Recipient: []byte(`{"email":"a@example.com"}`), Content: []byte(`{"subject":"hi"}`)},
		provider.Settings{},
	)
	assertPermanentError(t, err)
}

func TestEmailChannel_SendFailsPermanentlyForUnsupportedProvider(t *testing.T) {
	var ch provider.EmailChannel
	_, err := ch.Send(context.Background(),
		provider.Notification{Recipient: []byte(`{"email":"a@example.com"}`), Content: []byte(`{"subject":"hi"}`)},
		provider.Settings{Config: datatypes.JSON(`{"provider":"sendgrid"}`)},
	)
	assertPermanentError(t, err)
}

func TestEmailChannel_SendFailsPermanentlyForIncompleteSMTPConfig(t *testing.T) {
	var ch provider.EmailChannel
	_, err := ch.Send(context.Background(),
		provider.Notification{Recipient: []byte(`{"email":"a@example.com"}`), Content: []byte(`{"subject":"hi"}`)},
		provider.Settings{Config: datatypes.JSON(`{"provider":"smtp","host":"smtp.example.com"}`)},
	)
	assertPermanentError(t, err)
}

func TestEmailChannel_SendFailsPermanentlyForMalformedCredentials(t *testing.T) {
	var ch provider.EmailChannel
	_, err := ch.Send(context.Background(),
		provider.Notification{Recipient: []byte(`{"email":"a@example.com"}`), Content: []byte(`{"subject":"hi"}`)},
		provider.Settings{
			Config:      datatypes.JSON(`{"provider":"smtp","host":"smtp.example.com","port":2525,"from":"noreply@example.com"}`),
			Credentials: `not-json`,
		},
	)
	assertPermanentError(t, err)
}

func assertPermanentError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	var sendErr *provider.SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("expected *provider.SendError, got %T: %v", err, err)
	}
	if sendErr.Transient {
		t.Fatalf("expected a permanent (non-transient) error, got transient: %v", err)
	}
}

// fakeSMTPServer implements just enough of RFC 5321 (EHLO/MAIL/RCPT/DATA/QUIT,
// no STARTTLS/AUTH) to exercise EmailChannel's tls_mode:"none" send path
// end-to-end without a real mail server.
type fakeSMTPServer struct {
	addr     string
	received chan string
}

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := &fakeSMTPServer{addr: ln.Addr().String(), received: make(chan string, 1)}
	go srv.serveOne(t, ln)
	return srv
}

func (s *fakeSMTPServer) serveOne(t *testing.T, ln net.Listener) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writeLine := func(line string) {
		if _, err := fmt.Fprintf(conn, "%s\r\n", line); err != nil {
			t.Errorf("write: %v", err)
		}
	}

	writeLine("220 fake.smtp.local ESMTP")
	var data strings.Builder
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false
				s.received <- data.String()
				writeLine("250 OK: queued")
				continue
			}
			data.WriteString(line)
			data.WriteString("\n")
			continue
		}

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			writeLine("250 fake.smtp.local")
		case strings.HasPrefix(upper, "MAIL FROM"):
			writeLine("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			writeLine("250 OK")
		case strings.HasPrefix(upper, "DATA"):
			inData = true
			writeLine("354 Start mail input")
		case strings.HasPrefix(upper, "QUIT"):
			writeLine("221 Bye")
			return
		default:
			writeLine("500 unrecognized command")
		}
	}
}

func TestEmailChannel_SendDeliversOverPlaintextSMTP(t *testing.T) {
	srv := startFakeSMTPServer(t)
	host, port, err := net.SplitHostPort(srv.addr)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}

	var ch provider.EmailChannel
	config := fmt.Sprintf(`{"provider":"smtp","host":%q,"port":%s,"from":"noreply@example.com","from_name":"Example","tls_mode":"none"}`, host, port)

	result, err := ch.Send(context.Background(),
		provider.Notification{
			Recipient: []byte(`{"email":"a@example.com"}`),
			Content:   []byte(`{"subject":"Hi\r\nBcc: attacker@evil.com","body":"hello world"}`),
		},
		provider.Settings{Config: datatypes.JSON(config)},
	)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Provider != "smtp" || result.ProviderMessageID == "" {
		t.Fatalf("result = %+v", result)
	}

	select {
	case msg := <-srv.received:
		if !strings.Contains(msg, "From: Example <noreply@example.com>") {
			t.Fatalf("message missing From header: %s", msg)
		}
		if !strings.Contains(msg, "To: a@example.com") {
			t.Fatalf("message missing To header: %s", msg)
		}
		if !strings.Contains(msg, "hello world") {
			t.Fatalf("message missing body: %s", msg)
		}
		// The injected CRLF must have been stripped from the Subject value,
		// not smuggled in as a standalone "Bcc:" header line.
		for line := range strings.SplitSeq(msg, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Bcc:") {
				t.Fatalf("header injection was not sanitized, found injected header line %q in: %s", line, msg)
			}
		}
		if !strings.Contains(msg, "Subject: HiBcc: attacker@evil.com") {
			t.Fatalf("expected sanitized (CRLF-stripped, merged) subject line, got: %s", msg)
		}
	default:
		t.Fatal("fake smtp server never received a DATA payload")
	}
}
