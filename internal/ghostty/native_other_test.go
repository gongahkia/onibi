//go:build !darwin

package ghostty

import (
	"errors"
	"testing"
)

func TestUnsupportedPlatformFailsBeforeActions(t *testing.T) {
	if _, err := New().Targets(t.Context()); !errors.Is(err, ErrUnsupported) || !IsRejected(err) {
		t.Fatalf("err=%v", err)
	}
}
