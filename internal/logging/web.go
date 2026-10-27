package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// Web writes structured JSON to the private rotating file. A foreground
// console receives compact, monochrome events; background services use only
// the file. Callers must supply allowlisted metadata, never terminal content.
func Web(file, console io.Writer, level slog.Level) *slog.Logger {
	handlers := []slog.Handler{slog.NewJSONHandler(file, &slog.HandlerOptions{Level: level})}
	if console != nil {
		handlers = append(handlers, slog.NewJSONHandler(&consoleWriter{out: console}, &slog.HandlerOptions{Level: level}))
	}
	return slog.New(multiHandler(handlers))
}

type multiHandler []slog.Handler

func (h multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, x := range h {
		if x.Enabled(ctx, l) {
			return true
		}
	}
	return false
}
func (h multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var result error
	for _, x := range h {
		if x.Enabled(ctx, r.Level) {
			if err := x.Handle(ctx, r); err != nil {
				result = err
			}
		}
	}
	return result
}
func (h multiHandler) WithAttrs(a []slog.Attr) slog.Handler {
	out := make(multiHandler, len(h))
	for i, x := range h {
		out[i] = x.WithAttrs(a)
	}
	return out
}
func (h multiHandler) WithGroup(g string) slog.Handler {
	out := make(multiHandler, len(h))
	for i, x := range h {
		out[i] = x.WithGroup(g)
	}
	return out
}

// The same JSON record formatter is used for foreground events and log replay.
// Styling/activity belong to termui.Live, leaving the persisted JSON untouched.
type consoleWriter struct {
	out io.Writer
	mu  sync.Mutex
}

func (w *consoleWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := fmt.Fprintln(w.out, FormatRecord(bytes.TrimSpace(p)))
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
