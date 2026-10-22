//go:build darwin && !cgo

package ghostty

import (
	"context"
	"fmt"
)

func captureNative(context.Context, int) (string, error) {
	return "", fmt.Errorf("%w: use a macOS build with native Accessibility support", ErrCaptureUnavailable)
}
