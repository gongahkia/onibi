//go:build darwin

package ghostty

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os/exec"
	"time"
)

//go:embed control.js
var controlScript string

// User-controlled values are sent exclusively on stdin. They are never added
// to the script, process arguments, diagnostics, or an interpolated shell.
func runNative(ctx context.Context, req nativeRequest) (nativeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	payload, err := json.Marshal(req)
	if err != nil {
		return nativeResponse{}, rejected(errors.New("invalid Ghostty request"))
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", controlScript)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr boundedOutput
	stdout.limit, stderr.limit = 2*1024*1024, 4096
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nativeResponse{}, errors.New("Ghostty did not respond in time; inspect the terminal before trying again")
		}
		// Apple event errors can include arguments. Never return raw stderr.
		return nativeResponse{}, errors.New("Ghostty communication failed; inspect the terminal before trying again")
	}
	if stdout.overflow {
		return nativeResponse{}, errors.New("Ghostty response exceeded the output limit")
	}
	var result nativeResponse
	if err := json.Unmarshal(stdout.buf.Bytes(), &result); err != nil {
		return nativeResponse{}, errors.New("Ghostty returned an invalid response")
	}
	return result, nil
}

type boundedOutput struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := w.limit - w.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
			w.overflow = true
		}
		_, _ = w.buf.Write(p)
	} else if n > 0 {
		w.overflow = true
	}
	return n, nil
}
