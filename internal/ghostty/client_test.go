package ghostty

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
		{Kind: "close_tab", WindowID: testID("window", "w"), TabID: encodeID(123, "other-start", "tab", "t"), ExpectedTargets: []string{testID("terminal", "s")}},
		{Kind: "close_window", WindowID: testID("window", "w"), ExpectedTargets: []string{encodeID(123, "other-start", "terminal", "s")}},
		{Kind: "close_window", WindowID: testID("window", "w"), ExpectedTargets: []string{testID("tab", "wrong-kind")}},
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
		if r.Operation == "close_tab" && (len(r.ExpectedTargets) != 1 || r.ExpectedTargets[0] != "s") {
			t.Fatalf("lost reviewed identities: %#v", r)
		}
		return nativeResponse{Status: "ok", TargetID: "new-surface"}, nil
	}}
	r, err := c.Execute(t.Context(), Action{Kind: "new_tab", WindowID: testID("window", "w")})
	if err != nil || r.TargetID != testID("terminal", "new-surface") {
		t.Fatalf("result=%#v err=%v", r, err)
	}
	if _, err := c.Execute(t.Context(), Action{Kind: "close_tab", WindowID: testID("window", "w"), TabID: testID("tab", "t"), ExpectedTargets: []string{testID("terminal", "s")}}); err != nil {
		t.Fatal(err)
	}
}

func TestCockpitActionsRemainScopedAndAllowlisted(t *testing.T) {
	for _, a := range []Action{
		{Kind: "split", TargetID: testID("terminal", "s"), Direction: "right"},
		{Kind: "close_pane", TargetID: testID("terminal", "s"), ExpectedTargets: []string{testID("terminal", "s")}},
		{Kind: "close_window", WindowID: testID("window", "w"), ExpectedTargets: []string{testID("terminal", "s")}},
		{Kind: "rename_tab", TargetID: testID("terminal", "s"), Name: "agent: 'review'"},
		{Kind: "rename_pane", TargetID: testID("terminal", "s"), Name: ""},
		{Kind: "control", TargetID: testID("terminal", "s"), Control: "resize_right"},
		{Kind: "new_window"},
	} {
		r, err := prepareAction(a)
		if err != nil {
			t.Fatalf("%s: %v", a.Kind, err)
		}
		if a.Kind != "new_window" && (r.PID != 123 || r.Started == "") {
			t.Fatalf("lost process scope: %#v", r)
		}
		if a.Kind == "rename_tab" && r.NativeAction != "set_tab_title:"+a.Name {
			t.Fatalf("title was not literal: %#v", r)
		}
		if a.Kind == "control" && r.NativeAction != "resize_split:right,40" {
			t.Fatalf("wrong native control: %#v", r)
		}
	}
	for _, a := range []Action{
		{Kind: "new_window", TargetID: testID("terminal", "s")},
		{Kind: "split", TargetID: testID("terminal", "s"), Direction: "right; quit"},
		{Kind: "control", TargetID: testID("terminal", "s"), Control: "close_all_windows"},
		{Kind: "rename_tab", TargetID: testID("terminal", "s"), Name: "hidden\ncommand"},
		{Kind: "focus", TargetID: testID("terminal", "s"), Text: "unexpected"},
	} {
		if _, err := prepareAction(a); err == nil {
			t.Fatalf("accepted %#v", a)
		}
	}
}

func TestNewWindowUsesReturnedProcessIdentity(t *testing.T) {
	c := &Client{run: func(_ context.Context, r nativeRequest) (nativeResponse, error) {
		if r.Operation != "new_window" || r.PID != 0 {
			t.Fatalf("request=%#v", r)
		}
		return nativeResponse{Status: "ok", PID: 456, Started: "new-launch", TargetID: "created"}, nil
	}}
	r, err := c.Execute(t.Context(), Action{Kind: "new_window"})
	if err != nil || r.TargetID != encodeID(456, "new-launch", "terminal", "created") {
		t.Fatalf("result=%#v err=%v", r, err)
	}
}

func TestCancelledWaiterDoesNotQueueBehindNativeCapture(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	c := &Client{run: func(_ context.Context, r nativeRequest) (nativeResponse, error) {
		close(entered)
		<-release
		return nativeResponse{Status: "ok", PID: 123, Started: "launch"}, nil
	}}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = c.Targets(t.Context()) }()
	<-entered
	defer func() { close(release); <-done }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	waiter := make(chan error, 1)
	go func() {
		_, err := c.Execute(ctx, Action{Kind: "key", TargetID: testID("terminal", "s"), Key: "enter"})
		waiter <- err
	}()
	select {
	case err := <-waiter:
		if !errors.Is(err, context.Canceled) || !IsRejected(err) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled action stayed queued behind native work")
	}
}
