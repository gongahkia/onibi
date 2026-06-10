package logging

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
)

const placeholder = "[REDACTED]"

// secretsRef holds the active set of strings the redaction handler will
// scrub from every log record. Pointer-swapped atomically so SetSecrets is
// lock-free for readers. Strings should be the long-lived secret values
// (the bot token, TOTP secret in hex form, etc.); anything shorter than 12
// chars is ignored to avoid false positives.
var secretsRef atomic.Pointer[[]string]

func init() {
	empty := []string{}
	secretsRef.Store(&empty)
}

// SetSecrets replaces the redaction set. Strings shorter than 12 chars are
// dropped. Safe to call from any goroutine; takes effect on the next record.
func SetSecrets(s ...string) {
	out := make([]string, 0, len(s))
	for _, v := range s {
		if len(v) >= 12 {
			out = append(out, v)
		}
	}
	secretsRef.Store(&out)
}

// redactingHandler wraps another slog.Handler and rewrites any record
// whose Message or attribute values contain a tracked secret.
type redactingHandler struct {
	inner slog.Handler
}

// NewRedactingHandler wraps inner so that every Handle call scrubs tracked
// secrets from the message and from string-typed attribute values.
func NewRedactingHandler(inner slog.Handler) slog.Handler {
	return &redactingHandler{inner: inner}
}

func (h *redactingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &redactingHandler{inner: h.inner.WithAttrs(redactAttrs(attrs))}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name)}
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	secrets := *secretsRef.Load()
	if len(secrets) == 0 {
		return h.inner.Handle(ctx, r)
	}
	// rewrite message
	if msg := scrub(r.Message, secrets); msg != r.Message {
		r = cloneRecord(r, msg)
	}
	// rewrite attrs by walking and re-emitting
	scrubbed := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		scrubbed.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, scrubbed)
}

func cloneRecord(r slog.Record, msg string) slog.Record {
	c := slog.NewRecord(r.Time, r.Level, msg, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		c.AddAttrs(a)
		return true
	})
	return c
}

func redactAttrs(in []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(in))
	for i, a := range in {
		out[i] = redactAttr(a)
	}
	return out
}

func redactAttr(a slog.Attr) slog.Attr {
	secrets := *secretsRef.Load()
	if len(secrets) == 0 {
		return a
	}
	if a.Value.Kind() == slog.KindString {
		if s := scrub(a.Value.String(), secrets); s != a.Value.String() {
			return slog.String(a.Key, s)
		}
	}
	if a.Value.Kind() == slog.KindAny {
		if err, ok := a.Value.Any().(error); ok {
			if s := scrub(err.Error(), secrets); s != err.Error() {
				return slog.String(a.Key, s)
			}
		}
	}
	return a
}

func scrub(s string, secrets []string) string {
	for _, sec := range secrets {
		if sec == "" || len(sec) < 12 {
			continue
		}
		if strings.Contains(s, sec) {
			s = strings.ReplaceAll(s, sec, placeholder)
		}
	}
	return s
}
