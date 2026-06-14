package setup

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

func TestRunPairsOwnerWithMockTelegram(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "setup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	sec, err := secrets.Open(secrets.Options{
		EnvFallbackPath: filepath.Join(t.TempDir(), ".env"),
		PreferDotenv:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	oldFactory := newPairClient
	mockCh := make(chan *telegram.Mock, 1)
	newPairClient = func(_ context.Context, _ string, h telegram.HandlerFunc) (pairClient, error) {
		mock := telegram.NewMock(&models.User{ID: 123, Username: "onibi_test_bot", IsBot: true})
		mock.SetHandler(h)
		mockCh <- mock
		return mock, nil
	}
	t.Cleanup(func() { newPairClient = oldFactory })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := Run(ctx, db, sec, Flags{
			PairTimeout: time.Second,
		}, IO{
			In:  &oneByteReader{r: strings.NewReader("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nenabled\n")},
			Out: &bytes.Buffer{},
			Err: &bytes.Buffer{},
		})
		errCh <- err
	}()

	var mock *telegram.Mock
	select {
	case mock = <-mockCh:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	token := latestPairToken(t, db)
	mock.Dispatch(ctx, &models.Update{Message: &models.Message{
		From: &models.User{ID: 777},
		Chat: models.Chat{ID: 777, Type: "private"},
		Text: "/start " + PairPrefix + token,
	}})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	owner, ok, err := db.KVGetString(context.Background(), auth.KVKeyOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || owner != "777" {
		t.Fatalf("owner = %q ok=%v", owner, ok)
	}
	sent := mock.Sent()
	if len(sent) < 2 || !strings.Contains(sent[0].Text, "Paired") || !strings.Contains(sent[1].Text, "/new <agent>") {
		t.Fatalf("sent = %#v", sent)
	}
}

func TestRotateOwnerRequiresCurrentOwnerConfirmation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "setup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	owner := &auth.Owner{}
	if err := auth.SetOwner(context.Background(), db, owner, 111); err != nil {
		t.Fatal(err)
	}
	sec, err := secrets.Open(secrets.Options{
		EnvFallbackPath: filepath.Join(t.TempDir(), ".env"),
		PreferDotenv:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(secrets.KeyBotToken, "old-token"); err != nil {
		t.Fatal(err)
	}

	oldConfirmFactory := newRotateConfirmClient
	oldPairFactory := newPairClient
	confirmCh := make(chan *telegram.Mock, 1)
	pairCh := make(chan *telegram.Mock, 1)
	newRotateConfirmClient = func(_ context.Context, _ string, h telegram.HandlerFunc) (telegram.API, error) {
		mock := telegram.NewMock(&models.User{ID: 123, Username: "onibi_test_bot", IsBot: true})
		mock.SetHandler(h)
		confirmCh <- mock
		return mock, nil
	}
	newPairClient = func(_ context.Context, _ string, h telegram.HandlerFunc) (pairClient, error) {
		mock := telegram.NewMock(&models.User{ID: 123, Username: "onibi_test_bot", IsBot: true})
		mock.SetHandler(h)
		pairCh <- mock
		return mock, nil
	}
	t.Cleanup(func() {
		newRotateConfirmClient = oldConfirmFactory
		newPairClient = oldPairFactory
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := Run(ctx, db, sec, Flags{
			RotateOwner: true,
			PairTimeout: time.Second,
		}, IO{
			In:  &oneByteReader{r: strings.NewReader("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nenabled\n")},
			Out: &bytes.Buffer{},
			Err: &bytes.Buffer{},
		})
		errCh <- err
	}()

	var confirmMock *telegram.Mock
	select {
	case confirmMock = <-confirmCh:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	sent := waitSent(t, ctx, confirmMock, 1)
	if len(sent) != 1 {
		t.Fatalf("confirm sent = %#v", sent)
	}
	code := regexp.MustCompile(`/confirm-rotate ([0-9a-f]+)`).FindStringSubmatch(sent[0].Text)
	if len(code) != 2 {
		t.Fatalf("confirmation text = %q", sent[0].Text)
	}
	confirmMock.Dispatch(ctx, &models.Update{Message: &models.Message{
		From: &models.User{ID: 111},
		Chat: models.Chat{ID: 111, Type: "private"},
		Text: "/confirm-rotate " + code[1],
	}})

	var pairMock *telegram.Mock
	select {
	case pairMock = <-pairCh:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	token := latestPairToken(t, db)
	pairMock.Dispatch(ctx, &models.Update{Message: &models.Message{
		From: &models.User{ID: 777},
		Chat: models.Chat{ID: 777, Type: "private"},
		Text: "/start " + PairPrefix + token,
	}})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	got, ok, err := db.KVGetString(context.Background(), auth.KVKeyOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "777" {
		t.Fatalf("owner = %q ok=%v", got, ok)
	}
}

func TestValidateTokenShape(t *testing.T) {
	tests := []struct {
		name  string
		token string
		ok    bool
	}{
		{"valid35", "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", true},
		{"validLong", "123456789012:AAAA_BBBB-CCCCDDDD11112222333344445555", true},
		{"empty", "", false},
		{"digitsOnly", "123456789", false},
		{"noColon", "123456789AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", false},
		{"shortTail", "123456789:AAAA", false},
		{"specialChars", "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA!", false},
		{"shortID", "1234:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTokenShape(tt.token)
			if (err == nil) != tt.ok {
				t.Fatalf("validateTokenShape(%q) err=%v ok=%v", tt.token, err, tt.ok)
			}
		})
	}
}

func TestPromptTokenRejectsBadShape(t *testing.T) {
	_, err := promptToken(false, IO{
		In:  strings.NewReader("not-a-token\n"),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "BotFather token") {
		t.Fatalf("err = %v", err)
	}
}

func TestPromptTokenStdinRejectsBadShape(t *testing.T) {
	_, err := promptToken(true, IO{
		In:  strings.NewReader("not-a-token\n"),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "BotFather token") {
		t.Fatalf("err = %v", err)
	}
}

func TestAcknowledge2FATimeout(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "setup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err = acknowledge2FA(ctx, db, IO{
		In:  pr,
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assert2FAAck(t, db, "timeout")
}

func TestAcknowledge2FAEnabled(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "setup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = acknowledge2FA(context.Background(), db, IO{
		In:  strings.NewReader("enabled\n"),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assert2FAAck(t, db, "enabled")
}

func TestAcknowledge2FASkip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "setup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = acknowledge2FA(context.Background(), db, IO{
		In:  strings.NewReader("skip\n"),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assert2FAAck(t, db, "skipped")
}

func TestAcknowledge2FARetriesOnBadInput(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "setup.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = acknowledge2FA(context.Background(), db, IO{
		In:  strings.NewReader("foo\nskip\n"),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	assert2FAAck(t, db, "skipped")
}

type oneByteReader struct {
	r *strings.Reader
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}

func waitSent(t *testing.T, ctx context.Context, mock *telegram.Mock, n int) []tgbot.SendMessageParams {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		sent := mock.Sent()
		if len(sent) >= n {
			return sent
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func latestPairToken(t *testing.T, db *store.DB) string {
	t.Helper()
	var token string
	err := db.SQL().QueryRow(`SELECT token FROM pairing_tokens ORDER BY created_at DESC LIMIT 1`).Scan(&token)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func assert2FAAck(t *testing.T, db *store.DB, want string) {
	t.Helper()
	got, ok, err := db.KVGetString(context.Background(), "tg_2fa_ack")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != want {
		t.Fatalf("tg_2fa_ack = %q ok=%v, want %q", got, ok, want)
	}
}
