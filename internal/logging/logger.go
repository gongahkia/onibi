package logging

import (
	"io"
	"log/slog"
	"os"
)

// New returns a slog.Logger that writes text-formatted records to w with the
// given level. If w is nil, stderr.
func New(w io.Writer, lvl slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
}
