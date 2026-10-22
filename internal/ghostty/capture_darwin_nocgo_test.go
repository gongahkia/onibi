//go:build darwin && !cgo

package ghostty

import (
	"errors"
	"testing"
)

func TestNoCGOBuildReportsCaptureUnavailable(t *testing.T) {
	if _, err := captureNative(t.Context(), 123); !errors.Is(err, ErrCaptureUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
