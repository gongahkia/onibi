//go:build !darwin

package ghostty

import "context"

func inspectNative(context.Context) (environment, error) { return environment{}, ErrUnsupported }
func captureReadiness() Check                            { return Check{"capture", "unsupported", ErrUnsupported.Error()} }
