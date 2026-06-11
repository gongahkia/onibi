package setup

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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
	sent := confirmMock.Sent()
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

type oneByteReader struct {
	r *strings.Reader
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
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
