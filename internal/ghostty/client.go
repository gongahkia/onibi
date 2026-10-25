// Package ghostty controls existing macOS Ghostty surfaces by their native IDs.
// It never infers terminal identity from titles, process names, or pane order.
package ghostty

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Target struct {
	ID          string `json:"id"`
	WindowID    string `json:"windowId"`
	WindowTitle string `json:"windowTitle"`
	TabID       string `json:"tabId"`
	TabTitle    string `json:"tabTitle"`
	Title       string `json:"title"`
	CWD         string `json:"cwd"`
	Focused     bool   `json:"focused"`
}

type Snapshot struct {
	TargetID   string    `json:"targetId"`
	Text       string    `json:"text"`
	CapturedAt time.Time `json:"capturedAt"`
}

type Action struct {
	Kind      string `json:"kind"`
	TargetID  string `json:"targetId,omitempty"`
	WindowID  string `json:"windowId,omitempty"`
	TabID     string `json:"tabId,omitempty"`
	Text      string `json:"text,omitempty"`
	Key       string `json:"key,omitempty"`
	Enter     bool   `json:"enter,omitempty"`
	Direction string `json:"direction,omitempty"`
	Name      string `json:"name,omitempty"`
	Control   string `json:"control,omitempty"`
}

type Result struct {
	TargetID string `json:"targetId,omitempty"`
	Message  string `json:"message"`
}

var (
	ErrUnsupported        = errors.New("Ghostty control is available on macOS")
	ErrFocusRequired      = errors.New("focus this terminal in Ghostty before reading its output")
	ErrCaptureUnavailable = errors.New("terminal output capture is unavailable")
)

// rejectedError identifies failures proven to precede any input or UI mutation.
// Other execution errors are deliberately uncertain: callers must not replay.
type rejectedError struct{ err error }

func (e *rejectedError) Error() string { return e.err.Error() }
func (e *rejectedError) Unwrap() error { return e.err }
func rejected(err error) error         { return &rejectedError{err: err} }

func IsRejected(err error) bool {
	var target *rejectedError
	return errors.As(err, &target)
}

type nativeRequest struct {
	Operation    string `json:"operation"`
	PID          int    `json:"pid,omitempty"`
	Started      string `json:"started,omitempty"`
	TargetID     string `json:"targetId,omitempty"`
	WindowID     string `json:"windowId,omitempty"`
	TabID        string `json:"tabId,omitempty"`
	Text         string `json:"text,omitempty"`
	Key          string `json:"key,omitempty"`
	Modifiers    string `json:"modifiers,omitempty"`
	Enter        bool   `json:"enter,omitempty"`
	Direction    string `json:"direction,omitempty"`
	NativeAction string `json:"nativeAction,omitempty"`
}

type nativeResponse struct {
	Status   string   `json:"status"`
	Code     string   `json:"code"`
	PID      int      `json:"pid"`
	Started  string   `json:"started"`
	Targets  []Target `json:"targets"`
	TargetID string   `json:"targetId"`
}

type runner func(context.Context, nativeRequest) (nativeResponse, error)
type capturer func(context.Context, int) (string, error)

type Client struct {
	gateOnce sync.Once
	gate     chan struct{}
	run      runner
	capture  capturer
}

func New() *Client { return &Client{run: runNative, capture: captureNative} }

// Waiting for a slow Accessibility/Apple event must honor the request deadline.
// A mutex would leave disconnected pollers queued behind a frozen application.
func (c *Client) acquire(ctx context.Context) error {
	c.gateOnce.Do(func() { c.gate = make(chan struct{}, 1) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-c.gate
			return err
		}
		return nil
	}
}

type identity struct {
	PID     int    `json:"p"`
	Started string `json:"s"`
	Kind    string `json:"k"`
	Native  string `json:"n"`
}

func encodeID(pid int, started, kind, native string) string {
	b, _ := json.Marshal(identity{PID: pid, Started: started, Kind: kind, Native: native})
	return "g1." + base64.RawURLEncoding.EncodeToString(b)
}

func decodeID(value, kind string) (identity, error) {
	var id identity
	if !strings.HasPrefix(value, "g1.") || len(value) > 1024 {
		return id, rejected(errors.New("select a current Ghostty " + kind))
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "g1."))
	if err != nil || json.Unmarshal(b, &id) != nil || id.PID <= 0 || id.Started == "" || len(id.Started) > 64 || id.Kind != kind || id.Native == "" || len(id.Native) > 256 {
		return identity{}, rejected(errors.New("invalid Ghostty " + kind + " identity"))
	}
	return id, nil
}

func (c *Client) call(ctx context.Context, req nativeRequest) (nativeResponse, error) {
	if err := ctx.Err(); err != nil {
		return nativeResponse{}, rejected(err)
	}
	response, err := c.run(ctx, req)
	if err != nil {
		return nativeResponse{}, err
	}
	if response.Status == "ok" {
		return response, nil
	}
	message := map[string]string{
		"not_running":        ErrNotRunning.Error(),
		"stale_process":      "Ghostty restarted; refresh the terminal list",
		"missing_target":     "this Ghostty terminal is no longer available; refresh the terminal list",
		"missing_window":     "this Ghostty window is no longer available; refresh the terminal list",
		"missing_tab":        "this Ghostty tab is no longer available in that window; refresh the terminal list",
		"permission":         "allow Onibi to control Ghostty in macOS Privacy & Security → Automation",
		"disabled":           "enable macos-applescript in Ghostty configuration and reload it",
		"focus_required":     ErrFocusRequired.Error(),
		"too_many_targets":   "Ghostty has too many terminal surfaces to list safely",
		"query_failed":       "Ghostty could not be inspected; check that macos-applescript is enabled and Automation access is allowed",
		"action_failed":      "Ghostty did not confirm the action; inspect the terminal before trying again",
		"unsupported_action": "Ghostty did not accept this control; check your installed version",
	}[response.Code]
	if message == "" {
		message = "Ghostty returned an invalid response"
	}
	var failure error = errors.New(message)
	switch response.Code {
	case "focus_required":
		failure = ErrFocusRequired
	case "not_running":
		failure = ErrNotRunning
	case "permission":
		failure = ErrAutomation
	case "stale_process", "missing_target", "missing_window", "missing_tab":
		failure = ErrStaleTarget
	case "unsupported_action":
		failure = ErrUnsupportedAction
	}
	if response.Status == "rejected" {
		failure = rejected(failure)
	}
	return nativeResponse{}, failure
}

func (c *Client) Targets(ctx context.Context) ([]Target, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-c.gate }()
	r, err := c.call(ctx, nativeRequest{Operation: "targets"})
	if err != nil {
		return nil, err
	}
	if r.PID <= 0 || r.Started == "" || len(r.Targets) > 256 {
		return nil, errors.New("Ghostty returned invalid terminal identities")
	}
	for i := range r.Targets {
		t := &r.Targets[i]
		if t.ID == "" || t.WindowID == "" || t.TabID == "" {
			return nil, errors.New("Ghostty returned an incomplete terminal identity")
		}
		t.ID = encodeID(r.PID, r.Started, "terminal", t.ID)
		t.WindowID = encodeID(r.PID, r.Started, "window", t.WindowID)
		t.TabID = encodeID(r.PID, r.Started, "tab", t.TabID)
	}
	if r.Targets == nil {
		r.Targets = []Target{}
	}
	return r.Targets, nil
}

func (c *Client) Snapshot(ctx context.Context, targetID string) (Snapshot, error) {
	if err := c.acquire(ctx); err != nil {
		return Snapshot{}, err
	}
	defer func() { <-c.gate }()
	id, err := decodeID(targetID, "terminal")
	if err != nil {
		return Snapshot{}, err
	}
	req := nativeRequest{Operation: "focused", PID: id.PID, Started: id.Started, TargetID: id.Native}
	verify := func() error {
		r, err := c.call(ctx, req)
		if err != nil {
			return err
		}
		if r.PID != id.PID || r.Started != id.Started || r.TargetID != id.Native {
			return ErrFocusRequired
		}
		return nil
	}
	if err := verify(); err != nil {
		return Snapshot{}, err
	}
	text, err := c.capture(ctx, id.PID)
	if err != nil {
		return Snapshot{}, err
	}
	// Local focus changes can race a capture. Discard the result rather than
	// returning text under the wrong surface ID.
	if err := verify(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{TargetID: targetID, Text: boundedText(text), CapturedAt: time.Now().UTC()}, nil
}

func (c *Client) Execute(ctx context.Context, action Action) (Result, error) {
	if err := c.acquire(ctx); err != nil {
		return Result{}, rejected(err)
	}
	defer func() { <-c.gate }()
	req, err := prepareAction(action)
	if err != nil {
		return Result{}, err
	}
	r, err := c.call(ctx, req)
	if err != nil {
		return Result{}, err
	}
	result := Result{TargetID: action.TargetID, Message: map[string]string{"input": "Input sent", "key": "Key sent", "focus": "Terminal focused", "new_tab": "Tab created", "new_window": "Window created", "split": "Pane created", "close_pane": "Pane close requested; refresh to confirm", "close_tab": "Tab close requested; refresh to confirm", "close_window": "Window close requested; refresh to confirm", "rename_tab": "Tab title updated", "rename_pane": "Pane title updated", "control": "Ghostty control accepted"}[action.Kind]}
	if action.Kind == "new_tab" || action.Kind == "new_window" || action.Kind == "split" {
		if r.TargetID == "" {
			return Result{}, errors.New("Ghostty did not return the created terminal; refresh the terminal list")
		}
		if action.Kind == "new_window" {
			if r.PID <= 0 || r.Started == "" {
				return Result{}, errors.New("Ghostty did not confirm the new window identity")
			}
			req.PID, req.Started = r.PID, r.Started
		}
		result.TargetID = encodeID(req.PID, req.Started, "terminal", r.TargetID)
	}
	return result, nil
}

func prepareAction(a Action) (nativeRequest, error) {
	r := nativeRequest{Operation: a.Kind, Text: a.Text, Enter: a.Enter, Direction: a.Direction}
	if err := ValidateAction(a); err != nil {
		return r, rejected(err)
	}
	var id identity
	var err error
	switch a.Kind {
	case "input", "key", "focus", "split", "close_pane", "rename_tab", "rename_pane", "control":
		id, err = decodeID(a.TargetID, "terminal")
		r.TargetID = id.Native
	case "new_tab", "close_tab", "close_window":
		id, err = decodeID(a.WindowID, "window")
		r.WindowID = id.Native
	case "new_window":
		return r, nil
	default:
		return r, rejected(errors.New("unsupported Ghostty action"))
	}
	if err != nil {
		return r, err
	}
	r.PID, r.Started = id.PID, id.Started
	if a.Kind == "close_tab" {
		tab, err := decodeID(a.TabID, "tab")
		if err != nil {
			return r, err
		}
		if tab.PID != id.PID || tab.Started != id.Started {
			return r, rejected(errors.New("tab and window belong to different Ghostty instances"))
		}
		r.TabID = tab.Native
	}
	if a.Kind == "input" && (a.Text == "" || len(a.Text) > 64*1024 || !utf8.ValidString(a.Text) || strings.ContainsRune(a.Text, 0)) {
		return r, rejected(errors.New("input must contain 1–65536 bytes of valid text without NUL characters"))
	}
	if a.Kind == "key" {
		r.Key, r.Modifiers, err = nativeKey(a.Key)
		if err != nil {
			return r, rejected(err)
		}
	}
	if a.Kind == "control" {
		r.NativeAction, _ = controlAction(a.Control)
	}
	if a.Kind == "rename_tab" {
		r.NativeAction = "set_tab_title:" + a.Name
	}
	if a.Kind == "rename_pane" {
		r.NativeAction = "set_surface_title:" + a.Name
	}
	return r, nil
}

func nativeKey(raw string) (string, string, error) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if mapped, ok := map[string]string{"enter": "enter", "tab": "tab", "esc": "escape", "escape": "escape", "up": "arrowUp", "down": "arrowDown", "left": "arrowLeft", "right": "arrowRight", "backspace": "backspace", "delete": "delete", "home": "home", "end": "end", "pgup": "pageUp", "pgdn": "pageDown", "space": "space"}[key]; ok {
		return mapped, "", nil
	}
	if key == "shift-tab" {
		return "tab", "shift", nil
	}
	for prefix, modifier := range map[string]string{"ctrl-": "control", "meta-": "option"} {
		if len(key) == len(prefix)+1 && strings.HasPrefix(key, prefix) && key[len(prefix)] >= 'a' && key[len(prefix)] <= 'z' {
			return key[len(prefix):], modifier, nil
		}
	}
	for n := 1; n <= 12; n++ {
		if key == fmt.Sprintf("f%d", n) {
			return key, "", nil
		}
	}
	return "", "", errors.New("unsupported terminal key")
}

func boundedText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) > 64*1024 {
		s = s[len(s)-64*1024:]
		for len(s) > 0 && !utf8.RuneStart(s[0]) {
			s = s[1:]
		}
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 400 {
		s = strings.Join(lines[len(lines)-400:], "\n")
	}
	return s
}
