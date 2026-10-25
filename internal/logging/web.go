package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// Web writes structured JSON to the private rotating file. A foreground
// console receives compact, monochrome events; background services use only
// the file. Callers must supply allowlisted metadata, never terminal content.
func Web(file, console io.Writer, level slog.Level) *slog.Logger {
	handlers := []slog.Handler{slog.NewJSONHandler(file, &slog.HandlerOptions{Level: level})}
	if console != nil {
		handlers = append(handlers, &consoleHandler{out: console, mu: &sync.Mutex{}, level: level})
	}
	return slog.New(NewRedactingHandler(multiHandler(handlers)))
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

type consoleHandler struct {
	out   io.Writer
	mu    *sync.Mutex
	level slog.Level
	attrs []slog.Attr
	group string
}

func (h *consoleHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }
func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %-5s  %s", r.Time.Format("15:04:05"), r.Level.String(), strings.ReplaceAll(strings.TrimPrefix(r.Message, "web."), "_", " "))
	attrs := append([]slog.Attr{}, h.attrs...)
	r.Attrs(func(a slog.Attr) bool { attrs = append(attrs, a); return true })
	for _, a := range attrs {
		fmt.Fprintf(&b, "  %s%s=%s", h.group, a.Key, consoleValue(a.Value.Resolve().String()))
	}
	b.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}
func consoleValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
}
func (h *consoleHandler) WithAttrs(a []slog.Attr) slog.Handler {
	copy := *h
	copy.attrs = append(append([]slog.Attr{}, h.attrs...), a...)
	return &copy
}
func (h *consoleHandler) WithGroup(g string) slog.Handler {
	copy := *h
	copy.group += g + "."
	return &copy
}
