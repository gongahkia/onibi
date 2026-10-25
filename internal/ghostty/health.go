package ghostty

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotRunning        = errors.New("Ghostty is not running; open it on this Mac first")
	ErrAutomation        = errors.New("allow the process launching Onibi to control Ghostty in macOS Privacy & Security → Automation")
	ErrUnresponsive      = errors.New("Ghostty did not respond in time; inspect the terminal before trying again")
	ErrCommunication     = errors.New("Ghostty communication failed; inspect the terminal before trying again")
	ErrStaleTarget       = errors.New("this Ghostty destination changed; refresh the terminal list")
	ErrUnsupportedAction = errors.New("this control is unavailable in the installed Ghostty version or current context")
	ErrCapturePermission = fmt.Errorf("%w: allow the process launching Onibi in macOS Privacy & Security → Accessibility", ErrCaptureUnavailable)
)

// ErrorCode is a stable, non-sensitive capability/availability classification.
// It never exposes native stderr or caller-provided error text.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrFocusRequired):
		return "focus_required"
	case errors.Is(err, ErrCapturePermission):
		return "capture_permission"
	case errors.Is(err, ErrCaptureUnavailable):
		return "snapshot_unavailable"
	case errors.Is(err, ErrNotRunning):
		return "ghostty_not_running"
	case errors.Is(err, ErrAutomation):
		return "automation_permission"
	case errors.Is(err, ErrUnresponsive):
		return "ghostty_timeout"
	case errors.Is(err, ErrCommunication):
		return "ghostty_communication"
	case errors.Is(err, ErrStaleTarget):
		return "target_changed"
	case errors.Is(err, ErrUnsupportedAction):
		return "unsupported_control"
	case errors.Is(err, ErrUnsupported):
		return "unsupported_platform"
	case errors.Is(err, context.DeadlineExceeded):
		return "ghostty_timeout"
	default:
		return "ghostty_unavailable"
	}
}

type Capabilities struct {
	Version string `json:"version"`
	Known   bool   `json:"known"`
	Control bool   `json:"control"`
	Rename  bool   `json:"rename"`
}

// Unknown/development versions remain explicitly unknown. A version is evidence
// of API availability, not proof that permissions or the app currently work.
func CapabilitiesForVersion(version string) Capabilities {
	c := Capabilities{Version: version}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return c
	}
	var n [3]int
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return c
		}
		value, err := strconv.Atoi(p)
		if err != nil {
			return c
		}
		n[i] = value
	}
	c.Known = true
	c.Control = n[0] > 1 || n[0] == 1 && n[1] >= 3
	c.Rename = n[0] > 1 || n[0] == 1 && (n[1] > 3 || n[1] == 3 && n[2] >= 1)
	return c
}

type Check struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type Diagnostics struct {
	CheckedAt    time.Time    `json:"checkedAt"`
	Capabilities Capabilities `json:"capabilities"`
	Checks       []Check      `json:"checks"`
}

type environment struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Running   int    `json:"running"`
}

// Inspect is passive: app-bundle/process metadata and the calling process's
// Accessibility trust only. It sends no Ghostty Apple events and reads no text.
func Inspect(ctx context.Context) Diagnostics {
	env, err := inspectNative(ctx)
	return diagnosticsFor(env, err, captureReadiness())
}

func diagnosticsFor(env environment, err error, capture Check) Diagnostics {
	d := Diagnostics{CheckedAt: time.Now().UTC(), Capabilities: CapabilitiesForVersion(env.Version)}
	switch {
	case errors.Is(err, ErrUnsupported):
		d.Checks = append(d.Checks, Check{"installation", "unsupported", ErrUnsupported.Error()}, Check{"process", "unknown", "Ghostty process inspection requires macOS."})
	case err != nil:
		d.Checks = append(d.Checks, Check{"installation", "unknown", "App metadata could not be inspected."}, Check{"process", "unknown", "Process status could not be inspected."})
	default:
		if !env.Installed {
			d.Checks = append(d.Checks, Check{"installation", "missing", "Install Ghostty 1.3.1 or later on this Mac."})
		} else if d.Capabilities.Known && !d.Capabilities.Control {
			d.Checks = append(d.Checks, Check{"installation", "unsupported", "Ghostty 1.3.0 or later is required; title controls need 1.3.1."})
		} else {
			d.Checks = append(d.Checks, Check{"installation", "ready", "Ghostty app bundle found."})
		}
		switch env.Running {
		case 0:
			d.Checks = append(d.Checks, Check{"process", "stopped", "Open Ghostty in this Mac's graphical login session."})
		case 1:
			d.Checks = append(d.Checks, Check{"process", "running", "One Ghostty process found; responsiveness has not been tested."})
		default:
			d.Checks = append(d.Checks, Check{"process", "ambiguous", "Multiple Ghostty processes found; native control requires exactly one."})
		}
	}
	d.Checks = append(d.Checks, Check{"automation", "unknown", "Not probed. Run onibi web doctor --probe for a bounded inventory request; macOS may ask for Automation permission."}, capture)
	return d
}
