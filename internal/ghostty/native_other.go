//go:build !darwin

package ghostty

import "context"

func runNative(context.Context, nativeRequest) (nativeResponse, error) {
	return nativeResponse{}, rejected(ErrUnsupported)
}

func captureNative(context.Context, int) (string, error) { return "", ErrUnsupported }
