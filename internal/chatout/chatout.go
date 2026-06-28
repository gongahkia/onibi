package chatout

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Sleeper func(context.Context, time.Duration) error

func Chunks(text string, limit int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{"(empty)"}
	}
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	var out []string
	buf := make([]rune, 0, limit)
	for _, r := range text {
		buf = append(buf, r)
		if len(buf) >= limit {
			out = append(out, string(buf))
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		out = append(out, string(buf))
	}
	return out
}

func SendChunks(ctx context.Context, text string, limit int, pace time.Duration, sleep Sleeper, send func(context.Context, string) error) error {
	for i, chunk := range Chunks(text, limit) {
		if i > 0 {
			if err := Sleep(ctx, pace, sleep); err != nil {
				return err
			}
		}
		if err := send(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}

func RetryAfter(header http.Header, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return fallback
	}
	if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec >= 0 {
		return time.Duration(sec * float64(time.Second))
	}
	if when, err := http.ParseTime(raw); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
		return 0
	}
	return fallback
}

func Sleep(ctx context.Context, d time.Duration, fn func(context.Context, time.Duration) error) error {
	if d <= 0 {
		return nil
	}
	if fn != nil {
		return fn(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
