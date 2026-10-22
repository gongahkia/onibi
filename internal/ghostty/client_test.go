package ghostty

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func testID(kind, raw string) string { return encodeID(123, "1792570227.213331", kind, raw) }

func TestTargetsBindEveryIdentityToProcessIncarnation(t *testing.T) {
	c := &Client{run: func(_ context.Context, req nativeRequest) (nativeResponse, error) {
		if req.Operation != "targets" {
			t.Fatalf("request=%#v", req)
		}
		return nativeResponse{Status: "ok", PID: 123, Started: "1792570227.213331", Targets: []Target{{ID: "surface", WindowID: "window", TabID: "tab", Title: "same name", Focused: true}}}, nil
	}}
	targets, err := c.Targets(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != testID("terminal", "surface") || targets[0].WindowID != testID("window", "window") || targets[0].TabID != testID("tab", "tab") {
		t.Fatalf("targets=%#v", targets)
	}
	if testID("terminal", "surface") == encodeID(123, "different-start", "terminal", "surface") {
		t.Fatal("reused PID did not change identity")
	}
}

func TestExecuteRejectsInvalidActionsBeforeNativeCalls(t *testing.T) {
	c := &Client{run: func(context.Context, nativeRequest) (nativeResponse, error) {
		t.Fatal("invalid action reached native bridge")
		return nativeResponse{}, nil
	}}
	for _, a := range []Action{
		{Kind: "kill", TargetID: testID("terminal", "s")},
		{Kind: "input", TargetID: testID("terminal", "s")},
		{Kind: "input", TargetID: testID("terminal", "s"), Text: "\x00"},
		{Kind: "input", TargetID: testID("terminal", "s"), Text: strings.Repeat("x", 65537)},
		{Kind: "key", TargetID: testID("terminal", "s"), Key: "command-q"},
		{Kind: "focus", TargetID: testID("window", "w")},
		{Kind: "new_tab"},
		{Kind: "close_tab", WindowID: testID("window", "w"), TabID: encodeID(123, "other-start", "tab", "t")},
	} {
		if _, err := c.Execute(t.Context(), a); err == nil || !IsRejected(err) {
			t.Fatalf("action=%#v err=%v", a, err)
		}
	}
}

func TestInputRemainsLiteralAndScoped(t *testing.T) {
	text := "  '\n\"; do shell script \"touch /tmp/never\"; $(echo no) \\ \t"
	c := &Client{run: func(_ context.Context, r nativeRequest) (nativeResponse, error) {
		if r.Text != text || r.TargetID != "surface" || r.PID != 123 || r.Started != "1792570227.213331" || r.Enter {
			t.Fatalf("request=%#v", r)
		}
		return nativeResponse{Status: "ok"}, nil
	}}
	result, err := c.Execute(t.Context(), Action{Kind: "input", TargetID: testID("terminal", "surface"), Text: text})
	if err != nil || result.TargetID != testID("terminal", "surface") {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestNativeKeyNamesMatchGhosttyDictionary(t *testing.T) {
	for raw, want := range map[string][2]string{"up": {"arrowUp", ""}, "pgdn": {"pageDown", ""}, "shift-tab": {"tab", "shift"}, "ctrl-c": {"c", "control"}, "meta-x": {"x", "option"}, "f12": {"f12", ""}} {
		key, mods, err := nativeKey(raw)
		if err != nil || key != want[0] || mods != want[1] {
			t.Fatalf("%s=%s,%s,%v", raw, key, mods, err)
		}
	}
}

func TestNativeRejectionsAreDistinctFromUncertainActions(t *testing.T) {
	for _, status := range []string{"rejected", "uncertain"} {
		c := &Client{run: func(context.Context, nativeRequest) (nativeResponse, error) {
			return nativeResponse{Status: status, Code: "action_failed"}, nil
		}}
		_, err := c.Execute(t.Context(), Action{Kind: "key", TargetID: testID("terminal", "s"), Key: "enter"})
		if err == nil || IsRejected(err) != (status == "rejected") {
			t.Fatalf("status=%s err=%v rejected=%t", status, err, IsRejected(err))
		}
	}
}

func TestSnapshotRequiresExactFocusBeforeAndAfterCapture(t *testing.T) {
	for _, failAt := range []int{1, 2, 0} {
		calls, captures := 0, 0
		c := &Client{
			run: func(_ context.Context, r nativeRequest) (nativeResponse, error) {
				calls++
				if r.Operation != "focused" || r.TargetID != "surface" || r.PID != 123 {
					t.Fatalf("request=%#v", r)
				}
				if calls == failAt {
					return nativeResponse{Status: "rejected", Code: "focus_required"}, nil
				}
				return nativeResponse{Status: "ok", PID: 123, Started: "1792570227.213331", TargetID: "surface"}, nil
			},
			capture: func(_ context.Context, pid int) (string, error) {
				captures++
				if pid != 123 {
					t.Fatal("wrong process")
				}
				return "private captured text", nil
			},
		}
		snapshot, err := c.Snapshot(t.Context(), testID("terminal", "surface"))
		if failAt != 0 {
			if !errors.Is(err, ErrFocusRequired) || snapshot.Text != "" {
				t.Fatalf("snapshot=%#v err=%v", snapshot, err)
			}
			if failAt == 1 && captures != 0 {
				t.Fatal("captured a different focused terminal")
			}
		} else if err != nil || snapshot.Text != "private captured text" || snapshot.CapturedAt.IsZero() {
			t.Fatalf("snapshot=%#v err=%v", snapshot, err)
		}
	}
}

func TestSnapshotSurvivesMissingCaptureCapability(t *testing.T) {
	c := &Client{run: func(context.Context, nativeRequest) (nativeResponse, error) {
		return nativeResponse{Status: "ok", PID: 123, Started: "1792570227.213331", TargetID: "surface"}, nil
	}, capture: func(context.Context, int) (string, error) { return "", ErrCaptureUnavailable }}
	if _, err := c.Snapshot(t.Context(), testID("terminal", "surface")); !errors.Is(err, ErrCaptureUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestSnapshotRejectsMismatchedNativeIdentity(t *testing.T) {
	c := &Client{
		run: func(context.Context, nativeRequest) (nativeResponse, error) {
			return nativeResponse{Status: "ok", PID: 123, Started: "different-process", TargetID: "surface"}, nil
		},
		capture: func(context.Context, int) (string, error) { t.Fatal("captured an unverified process"); return "", nil },
	}
	if _, err := c.Snapshot(t.Context(), testID("terminal", "surface")); !errors.Is(err, ErrFocusRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestCancelledActionDoesNotStartNativeWork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := &Client{run: func(context.Context, nativeRequest) (nativeResponse, error) {
		t.Fatal("cancelled action reached the native bridge")
		return nativeResponse{}, nil
	}}
	_, err := c.Execute(ctx, Action{Kind: "key", TargetID: testID("terminal", "surface"), Key: "enter"})
	if !errors.Is(err, context.Canceled) || !IsRejected(err) {
		t.Fatalf("err=%v rejected=%t", err, IsRejected(err))
	}
}

func TestSnapshotBoundedUTF8Tail(t *testing.T) {
	if got := boundedText(strings.Repeat("一", 40000)); len(got) > 65536 || !utf8.ValidString(got) {
		t.Fatalf("length=%d valid=%t", len(got), utf8.ValidString(got))
	}
	if got := boundedText(strings.Repeat("line\n", 600) + "last"); len(strings.Split(got, "\n")) > 400 || !strings.HasSuffix(got, "last") {
		t.Fatal("tail was not bounded")
	}
}

func TestTabCreationReturnsBoundTerminalAndClosureKeepsWindowScope(t *testing.T) {
	c := &Client{run: func(_ context.Context, r nativeRequest) (nativeResponse, error) {
		if r.WindowID != "w" || r.PID != 123 {
			t.Fatalf("request=%#v", r)
		}
		if r.Operation == "close_tab" && r.TabID != "t" {
			t.Fatalf("request=%#v", r)
		}
		return nativeResponse{Status: "ok", TargetID: "new-surface"}, nil
	}}
	r, err := c.Execute(t.Context(), Action{Kind: "new_tab", WindowID: testID("window", "w")})
	if err != nil || r.TargetID != testID("terminal", "new-surface") {
		t.Fatalf("result=%#v err=%v", r, err)
	}
	if _, err := c.Execute(t.Context(), Action{Kind: "close_tab", WindowID: testID("window", "w"), TabID: testID("tab", "t")}); err != nil {
		t.Fatal(err)
	}
}
