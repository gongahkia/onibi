package daemon

import (
	"context"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time             { return c.t }
func (c *fakeClock) advance(step time.Duration) { c.t = c.t.Add(step) }

func testTracker() (*attentionTracker, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)}
	tr := newAttentionTracker()
	tr.now = clock.now
	return tr, clock
}

func TestAttentionMovesFromQuietToActiveToIdle(t *testing.T) {
	tr, clock := testTracker()
	if got := tr.observe("s", attentionObservation{Plain: "$ "}); got.State != AttentionQuiet {
		t.Fatalf("baseline=%#v", got)
	}
	clock.advance(time.Second)
	got := tr.observe("s", attentionObservation{Plain: "$ make\nbuilding"})
	if got.State != AttentionActive || !got.LastOutputAt.Equal(clock.t) {
		t.Fatalf("after output=%#v", got)
	}
	activeSince := got.Since
	clock.advance(10 * time.Second)
	if got := tr.observe("s", attentionObservation{Plain: "$ make\nbuilding"}); got.State != AttentionActive || !got.Since.Equal(activeSince) {
		t.Fatalf("still active=%#v", got)
	}
	clock.advance(defaultIdleAfter)
	if got := tr.observe("s", attentionObservation{Plain: "$ make\nbuilding"}); got.State != AttentionIdle || !got.Since.Equal(clock.t) {
		t.Fatalf("idle=%#v", got)
	}
}

func TestAttentionDetectsPromptsInRecentLines(t *testing.T) {
	for _, tc := range []struct {
		plain, want string
	}{
		{"Removing 3 files\nContinue? [y/N] ", "Yes/no prompt"},
		{"Overwrite config (yes/no)?", "Yes/no prompt"},
		{"Done.\nPress Enter to exit", "Waiting for a key press"},
		{"ssh host\nEnter passphrase for key '/k':  ", "Password prompt"},
		{"Do you want to apply these changes?\n\n  1. Yes\n  2. No\n", "Question prompt"},
		{"Would you like to continue?\n❯ 1. Yes\n  2. No, and tell it what to do", "Question prompt"},
		{"Are you sure you want to delete branch main?", "Question prompt"},
		{"password: hunter2 was rejected\n$ ", ""},
		{"Continue? [y/N] y\n$ ", ""},
		{"Continue? [y/N] y", ""},
		{"% read -p 'Overwrite config? [y/N] ' a\nread: -p: no coprocess\n% ", ""},
		{"% read -p 'Overwrite config? [y/N] ' a", ""},
		{"Do you want to apply these changes?\n  1. Yes\n$ ", ""},
		{"Would you like to know more?\nSee the docs.", ""},
		{"compiling\nok", ""},
	} {
		if got := detectPrompt(tc.plain); got != tc.want {
			t.Errorf("detectPrompt(%q)=%q want %q", tc.plain, got, tc.want)
		}
	}
	tr, _ := testTracker()
	if got := tr.observe("s", attentionObservation{Plain: "Proceed? [y/N]"}); got.State != AttentionInput || got.Reason != "Yes/no prompt" {
		t.Fatalf("prompt on first observation=%#v", got)
	}
}

func TestAttentionBellCounterIgnoresBaselineAndClearsOnInput(t *testing.T) {
	tr, clock := testTracker()
	tr.observe("s", attentionObservation{Plain: "x", Bells: 4, BellsCounted: true})
	if got := tr.observe("s", attentionObservation{Plain: "x", Bells: 4, BellsCounted: true}); got.State == AttentionBell {
		t.Fatalf("bells from before the baseline were reported: %#v", got)
	}
	clock.advance(time.Second)
	if got := tr.observe("s", attentionObservation{Plain: "x", Bells: 5, BellsCounted: true}); got.State != AttentionBell {
		t.Fatalf("new bell=%#v", got)
	}
	tr.acknowledge("s")
	if got := tr.snapshot("s"); got.State == AttentionBell {
		t.Fatalf("acknowledged bell still pending: %#v", got)
	}
	if got := tr.observe("s", attentionObservation{Plain: "x", Bells: 5, BellsCounted: true}); got.State == AttentionBell {
		t.Fatalf("acknowledged bell came back: %#v", got)
	}
}

func TestAttentionBellClearsOnNewOutputAndFlagUsesRisingEdge(t *testing.T) {
	tr, _ := testTracker()
	tr.observe("s", attentionObservation{Plain: "a", BellFlag: true})
	if got := tr.observe("s", attentionObservation{Plain: "a", BellFlag: true}); got.State == AttentionBell {
		t.Fatalf("pre-existing flag reported: %#v", got)
	}
	tr.observe("s", attentionObservation{Plain: "a"})
	if got := tr.observe("s", attentionObservation{Plain: "a", BellFlag: true}); got.State != AttentionBell {
		t.Fatalf("rising flag=%#v", got)
	}
	if got := tr.observe("s", attentionObservation{Plain: "b", BellFlag: true}); got.State != AttentionActive {
		t.Fatalf("output after bell=%#v", got)
	}
}

func TestDaemonAttentionWatcherReadsTmuxWithoutTouchingActivity(t *testing.T) {
	d, runner := testDaemon(t)
	s := addTmuxSession(t, d, "aaaa1111", "build")
	before := s.LastActivityAt()
	screen := "working\n"
	runner.capture = func(context.Context) ([]byte, error) { return []byte(screen), nil }
	runner.alerts = "0 0\n"
	d.observeAttention(t.Context())
	screen = "\x1b[1mApply?\x1b[0m [y/N] "
	d.observeAttention(t.Context())
	infos := d.SessionInfos()
	if len(infos) != 1 || infos[0].Attention.State != AttentionInput {
		t.Fatalf("infos=%#v", infos)
	}
	if !s.LastActivityAt().Equal(before) {
		t.Fatal("background observation counted as session activity")
	}
	if err := d.SendSessionKey(t.Context(), s.ID, "y"); err != nil {
		t.Fatal(err)
	}
	d.markSessionEndedReason(t.Context(), s, "test")
	if got := d.attention.snapshot(s.ID); got.State != "" {
		t.Fatalf("ended session kept attention state: %#v", got)
	}
}
