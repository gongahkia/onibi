package daemon

import (
	"context"
	"crypto/sha256"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/mux"
	"github.com/gongahkia/onibi/internal/render"
)

const (
	AttentionInput  = "input"
	AttentionBell   = "bell"
	AttentionActive = "active"
	AttentionIdle   = "idle"
	AttentionQuiet  = "quiet"

	attentionCaptureLines = 40
	defaultIdleAfter      = 30 * time.Second
	promptWindowLines     = 6
)

// Prompt patterns are deliberately generic: they describe common terminal
// conventions rather than any particular program's interface.
var promptPatterns = []struct {
	re       *regexp.Regexp
	reason   string
	lastLine bool
}{
	{regexp.MustCompile(`(?i)[\[(]\s*(y/n|yes/no)\s*[\])]`), "Yes/no prompt", false},
	{regexp.MustCompile(`(?i)\bpress (enter|return|any key)\b`), "Waiting for a key press", false},
	{regexp.MustCompile(`(?i)\b(password|passphrase|passcode)\b[^:]*:\s*$`), "Password prompt", true},
	{regexp.MustCompile(`(?i)\b(do you want to|would you like to|are you sure|continue\?|proceed\?|overwrite\?)`), "Question prompt", false},
}

type attentionObservation struct {
	Plain        string
	Bells        int
	BellsCounted bool
	BellFlag     bool
}

type attentionState struct {
	seen        bool
	fingerprint [sha256.Size]byte
	lastOutput  time.Time
	bells       int
	bellFlag    bool
	bellPending bool
	current     intake.Attention
}

type attentionTracker struct {
	mu        sync.Mutex
	now       func() time.Time
	idleAfter time.Duration
	sessions  map[string]*attentionState
}

func newAttentionTracker() *attentionTracker {
	return &attentionTracker{now: time.Now, idleAfter: defaultIdleAfter, sessions: map[string]*attentionState{}}
}

// observe folds one capture into the session's attention state. The first
// observation after a daemon start only establishes a baseline, so bells and
// output from before the restart are not reported as new.
func (t *attentionTracker) observe(id string, obs attentionObservation) intake.Attention {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	st := t.sessions[id]
	if st == nil {
		st = &attentionState{}
		t.sessions[id] = st
	}
	fingerprint := sha256.Sum256([]byte(obs.Plain))
	first := !st.seen
	changed := !first && fingerprint != st.fingerprint
	rang := false
	if obs.BellsCounted {
		rang = !first && obs.Bells > st.bells
		st.bells = obs.Bells
	} else {
		rang = !first && obs.BellFlag && !st.bellFlag
	}
	st.bellFlag = obs.BellFlag
	st.seen, st.fingerprint = true, fingerprint
	if changed {
		st.lastOutput = now
		st.bellPending = false
	}
	if rang {
		st.bellPending = true
	}
	next := intake.Attention{LastOutputAt: st.lastOutput}
	if reason := detectPrompt(obs.Plain); reason != "" {
		next.State, next.Reason = AttentionInput, reason
	} else if st.bellPending {
		next.State, next.Reason = AttentionBell, "Terminal bell"
	} else if !st.lastOutput.IsZero() && now.Sub(st.lastOutput) < t.idleAfter {
		next.State = AttentionActive
	} else if !st.lastOutput.IsZero() {
		next.State, next.Reason = AttentionIdle, "No new output since its last change"
	} else {
		next.State = AttentionQuiet
	}
	if next.State == st.current.State && next.Reason == st.current.Reason {
		next.Since = st.current.Since
	} else {
		next.Since = now
	}
	st.current = next
	return next
}

// acknowledge clears a pending bell after someone sends the session input.
func (t *attentionTracker) acknowledge(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st := t.sessions[id]; st != nil && st.bellPending {
		st.bellPending = false
		if st.current.State == AttentionBell {
			st.current = intake.Attention{State: AttentionActive, Since: t.now(), LastOutputAt: st.lastOutput}
		}
	}
}

func (t *attentionTracker) forget(id string) {
	t.mu.Lock()
	delete(t.sessions, id)
	t.mu.Unlock()
}

func (t *attentionTracker) snapshot(id string) intake.Attention {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st := t.sessions[id]; st != nil {
		return st.current
	}
	return intake.Attention{}
}

func detectPrompt(plain string) string {
	lines := strings.Split(strings.TrimRight(plain, " \t\r\n"), "\n")
	var recent []string
	for i := len(lines) - 1; i >= 0 && len(recent) < promptWindowLines; i-- {
		if line := strings.TrimRight(lines[i], " \t\r"); strings.TrimSpace(line) != "" {
			recent = append(recent, line)
		}
	}
	for _, pattern := range promptPatterns {
		for i, line := range recent {
			if pattern.lastLine && i > 0 {
				break
			}
			if pattern.re.MatchString(line) {
				return pattern.reason
			}
		}
	}
	return ""
}

// installBellCounter is best effort: without it the watcher still sees the
// first bell through the window bell flag.
func (d *Daemon) installBellCounter(ctx context.Context, s *Session) {
	ctrl, err := d.tmuxControllerForCWD(s.CWD)
	if err == nil {
		err = ctrl.InstallBellCounter(ctx, mux.ParseTarget(s.TmuxTarget).Session)
	}
	if err != nil {
		d.Log.Warn("tmux bell counter unavailable", "session", s.ID, "err", err)
	}
}

func (d *Daemon) observeAttention(ctx context.Context) {
	for _, s := range d.liveSessions() {
		if !isTerminalSession(s) || s.TmuxTarget == "" {
			continue
		}
		obs, err := d.attentionObservation(ctx, s)
		if err != nil {
			d.Log.Debug("attention capture", "session", s.ID, "err", err)
			continue
		}
		before := d.attention.snapshot(s.ID)
		after := d.attention.observe(s.ID, obs)
		if after.State != before.State && (after.State == AttentionInput || after.State == AttentionBell) {
			d.Log.Info("session needs attention", "session", s.ID, "name", s.Name, "state", after.State, "reason", after.Reason)
		}
	}
}

func (d *Daemon) attentionObservation(ctx context.Context, s *Session) (attentionObservation, error) {
	var obs attentionObservation
	var raw string
	if s.Transport == "tmux" {
		ctrl, err := d.tmuxControllerForCWD(s.CWD)
		if err != nil {
			return obs, err
		}
		if raw, err = ctrl.Capture(ctx, s.TmuxTarget, attentionCaptureLines); err != nil {
			return obs, err
		}
		if obs.Bells, obs.BellsCounted, obs.BellFlag, err = ctrl.Alerts(ctx, s.TmuxTarget); err != nil {
			d.Log.Debug("tmux alerts", "session", s.ID, "err", err)
		}
	} else {
		ctrl, err := d.controllerForSession(s)
		if err != nil {
			return obs, err
		}
		if raw, err = ctrl.Capture(ctx, mux.ParseTarget(s.TmuxTarget), attentionCaptureLines); err != nil {
			return obs, err
		}
	}
	obs.Plain = string(render.StripANSI([]byte(raw)))
	return obs, nil
}
