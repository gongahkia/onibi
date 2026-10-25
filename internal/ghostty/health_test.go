package ghostty

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestVersionCapabilitiesDoNotPromiseUnsupportedTitles(t *testing.T) {
	for _, tt := range []struct {
		version                string
		known, control, rename bool
	}{
		{"1.2.3", true, false, false}, {"1.3.0", true, true, false}, {"1.3.1", true, true, true}, {"1.4.0", true, true, true}, {"2.0.0", true, true, true},
		{"", false, false, false}, {"1.3.1-dev", false, false, false}, {"1.3", false, false, false}, {"-1.3.1", false, false, false}, {"1.3.9999999999999999999999", false, false, false},
	} {
		t.Run(tt.version, func(t *testing.T) {
			got := CapabilitiesForVersion(tt.version)
			if got.Known != tt.known || got.Control != tt.control || got.Rename != tt.rename {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestPassiveDiagnosticsNeverClaimAutomationIsReady(t *testing.T) {
	for _, tt := range []struct {
		env     environment
		err     error
		process string
	}{
		{environment{true, "1.3.1", 1}, nil, "running"},
		{environment{}, nil, "stopped"},
		{environment{true, "1.3.0", 2}, nil, "ambiguous"},
		{environment{}, errors.New("secret native stderr"), "unknown"},
		{environment{}, ErrUnsupported, "unknown"},
	} {
		d := diagnosticsFor(tt.env, tt.err, Check{"capture", "unknown", "Not checked"})
		if d.Checks[1].State != tt.process || d.Checks[2].State != "unknown" {
			t.Fatalf("%+v", d)
		}
		for _, c := range d.Checks {
			if c.Detail == "secret native stderr" {
				t.Fatal("leaked native error")
			}
		}
	}
}

func TestErrorClassificationSurvivesWrapping(t *testing.T) {
	for err, code := range map[error]string{ErrNotRunning: "ghostty_not_running", ErrAutomation: "automation_permission", ErrUnresponsive: "ghostty_timeout", ErrCapturePermission: "capture_permission", ErrCaptureUnavailable: "snapshot_unavailable", ErrStaleTarget: "target_changed", ErrUnsupportedAction: "unsupported_control", context.DeadlineExceeded: "ghostty_timeout"} {
		if got := ErrorCode(rejected(fmt.Errorf("wrapped: %w", err))); got != code {
			t.Fatalf("%v: %s", err, got)
		}
	}
	if ErrorCode(errors.New("token=secret")) != "ghostty_unavailable" {
		t.Fatal("unclassified error exposed")
	}
	if !errors.Is(ErrCapturePermission, ErrCaptureUnavailable) {
		t.Fatal("capture capability contract lost")
	}
}
