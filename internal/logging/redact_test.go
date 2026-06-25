package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

const fakeToken = "1234567890:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef"

func newBuffered() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(NewRedactingHandler(inner)), &buf
}

func TestRedactsTokenInMessage(t *testing.T) {
	SetSecrets(fakeToken)
	t.Cleanup(func() { SetSecrets() })

	log, buf := newBuffered()
	log.Info("calling " + fakeToken + " now")

	if strings.Contains(buf.String(), fakeToken) {
		t.Fatalf("token leaked in message: %q", buf.String())
	}
	if !strings.Contains(buf.String(), placeholder) {
		t.Fatalf("expected placeholder, got %q", buf.String())
	}
}

func TestRedactsTokenInStringAttr(t *testing.T) {
	SetSecrets(fakeToken)
	t.Cleanup(func() { SetSecrets() })

	log, buf := newBuffered()
	log.Info("url", slog.String("url", "https://example.invalid/secret/"+fakeToken))

	if strings.Contains(buf.String(), fakeToken) {
		t.Fatalf("token leaked in attr: %q", buf.String())
	}
}

func TestRedactsTokenInErrorAttr(t *testing.T) {
	SetSecrets(fakeToken)
	t.Cleanup(func() { SetSecrets() })

	log, buf := newBuffered()
	err := errors.New("send failed for " + fakeToken)
	log.Error("secret", slog.Any("err", err))

	if strings.Contains(buf.String(), fakeToken) {
		t.Fatalf("token leaked in error: %q", buf.String())
	}
}

func TestShortSecretsIgnored(t *testing.T) {
	SetSecrets("short")
	t.Cleanup(func() { SetSecrets() })

	log, buf := newBuffered()
	log.Info("contains short string")

	if strings.Contains(buf.String(), placeholder) {
		t.Fatalf("short string should not have triggered redaction: %q", buf.String())
	}
}

func TestNoSecretsIsNoOp(t *testing.T) {
	SetSecrets()
	log, buf := newBuffered()
	log.Info("plain log")
	if !strings.Contains(buf.String(), "plain log") {
		t.Fatalf("expected message intact, got %q", buf.String())
	}
}
