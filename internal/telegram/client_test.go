package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func newPollTestClient(t *testing.T, handler func(*models.Update)) *Client {
	t.Helper()
	b, err := tgbot.New("123:abc",
		tgbot.WithSkipGetMe(),
		tgbot.WithNotAsyncHandlers(),
		tgbot.WithDefaultHandler(func(_ context.Context, _ *tgbot.Bot, update *models.Update) {
			if handler != nil {
				handler(update)
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &Client{Bot: b, allowed: AllowedUpdateTypes}
}

func TestPollLoopWarnsAfterTenEmptyPolls(t *testing.T) {
	c := newPollTestClient(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.AwaitOwnerInteraction(100, time.Minute)
	calls := 0
	warnings := 0
	c.poll = func(context.Context, int64, time.Duration, []string) ([]*models.Update, error) {
		calls++
		if calls == ownerRaceEmptyPollThreshold {
			cancel()
		}
		return nil, nil
	}
	c.warningSender = func(_ context.Context, chatID int64, text string) error {
		warnings++
		if chatID != 100 {
			t.Fatalf("chatID = %d", chatID)
		}
		if !strings.Contains(text, "onibi rotate-token") {
			t.Fatalf("warning = %q", text)
		}
		return nil
	}
	c.Start(ctx)
	if warnings != 1 {
		t.Fatalf("warnings = %d", warnings)
	}
}

func TestPollLoopInboundUpdateResetsEmptyPollCount(t *testing.T) {
	handled := 0
	c := newPollTestClient(t, func(*models.Update) { handled++ })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.AwaitOwnerInteraction(100, time.Minute)
	calls := 0
	warnings := 0
	c.poll = func(context.Context, int64, time.Duration, []string) ([]*models.Update, error) {
		calls++
		switch {
		case calls <= ownerRaceEmptyPollThreshold-1:
			return nil, nil
		case calls == ownerRaceEmptyPollThreshold:
			return []*models.Update{{ID: 5, Message: &models.Message{Text: "ok"}}}, nil
		case calls < ownerRaceEmptyPollThreshold*2:
			return nil, nil
		default:
			cancel()
			return nil, nil
		}
	}
	c.warningSender = func(context.Context, int64, string) error {
		warnings++
		return nil
	}
	c.Start(ctx)
	if handled != 1 {
		t.Fatalf("handled = %d", handled)
	}
	if warnings != 0 {
		t.Fatalf("warnings = %d", warnings)
	}
}

func TestPollLoopTransientErrorBackoffContinues(t *testing.T) {
	handled := 0
	c := newPollTestClient(t, func(*models.Update) { handled++ })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	var sleeps []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) bool {
		sleeps = append(sleeps, d)
		return true
	}
	c.poll = func(context.Context, int64, time.Duration, []string) ([]*models.Update, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("temporary")
		}
		cancel()
		return []*models.Update{{ID: 7, Message: &models.Message{Text: "ok"}}}, nil
	}
	c.Start(ctx)
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	if handled != 1 {
		t.Fatalf("handled = %d", handled)
	}
	if len(sleeps) != 1 || sleeps[0] != 100*time.Millisecond {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestPollLoopContextCancellationExits(t *testing.T) {
	c := newPollTestClient(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.poll = func(context.Context, int64, time.Duration, []string) ([]*models.Update, error) {
		t.Fatal("poll called after cancellation")
		return nil, nil
	}
	c.Start(ctx)
}
