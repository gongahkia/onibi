package telegram

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"
)

type RateLimiter struct {
	mu     sync.Mutex
	global bucket
	chats  map[int64]*bucket
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
}

type bucket struct {
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
}

func DefaultRateLimiter() *RateLimiter {
	return NewRateLimiter(25, 1)
}

func NewRateLimiter(globalPerSecond, chatPerSecond int) *RateLimiter {
	l := &RateLimiter{
		chats: map[int64]*bucket{},
		now:   time.Now,
		sleep: sleepContext,
	}
	if globalPerSecond > 0 {
		l.global = newBucket(globalPerSecond, globalPerSecond)
	}
	if chatPerSecond > 0 {
		b := newBucket(chatPerSecond, 1)
		l.chats[0] = &b
	}
	return l
}

func (l *RateLimiter) Wait(ctx context.Context, chatID int64) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	now := l.now()
	wait := l.global.reserve(now)
	if chatID != 0 {
		chat := l.chatBucket(chatID)
		if w := chat.reserve(now); w > wait {
			wait = w
		}
	}
	l.mu.Unlock()
	if wait > 0 {
		if err := l.sleep(ctx, wait); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (l *RateLimiter) chatBucket(chatID int64) *bucket {
	template, ok := l.chats[0]
	if !ok || template.rate <= 0 {
		return nil
	}
	b, ok := l.chats[chatID]
	if !ok {
		cp := *template
		b = &cp
		l.chats[chatID] = b
	}
	return b
}

func newBucket(ratePerSecond, capacity int) bucket {
	return bucket{
		rate:     float64(ratePerSecond) / float64(time.Second),
		capacity: float64(capacity),
		tokens:   float64(capacity),
	}
}

func (b *bucket) reserve(now time.Time) time.Duration {
	if b == nil || b.rate <= 0 {
		return 0
	}
	if b.last.IsZero() {
		b.last = now
	}
	elapsed := now.Sub(b.last)
	if elapsed > 0 {
		b.tokens += float64(elapsed) * b.rate
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}
	b.tokens--
	if b.tokens >= 0 {
		return 0
	}
	return time.Duration(-b.tokens / b.rate)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func chatIDKey(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case uint:
		return int64(x)
	case uint64:
		return int64(x)
	case string:
		h := fnv.New64a()
		_, _ = h.Write([]byte(x))
		return int64(h.Sum64())
	default:
		h := fnv.New64a()
		_, _ = h.Write([]byte(fmt.Sprint(v)))
		return int64(h.Sum64())
	}
}
