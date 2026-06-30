package web

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gongahkia/onibi/internal/pty"
)

const DefaultRecordingMaxBytes int64 = 50 << 20

type Recorder struct {
	Dir      string
	MaxBytes int64
	Rows     uint16
	Cols     uint16

	mu     sync.Mutex
	active map[string]bool
}

func NewRecorder(dir string) *Recorder {
	return &Recorder{
		Dir:      strings.TrimSpace(dir),
		MaxBytes: DefaultRecordingMaxBytes,
		Rows:     pty.DefaultRows,
		Cols:     pty.DefaultCols,
		active:   map[string]bool{},
	}
}

func (r *Recorder) Path(sessionID string) string {
	return filepath.Join(r.Dir, safeRecordingName(sessionID)+".cast")
}

func (r *Recorder) Record(ctx context.Context, sessionID string, host *pty.Host) error {
	if r == nil || strings.TrimSpace(r.Dir) == "" {
		return nil
	}
	if host == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("recording session id required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.active == nil {
		r.active = map[string]bool{}
	}
	if r.active[sessionID] {
		r.mu.Unlock()
		return nil
	}
	r.active[sessionID] = true
	r.mu.Unlock()
	w, err := newCastWriter(r.Path(sessionID), sessionID, r.maxBytes(), r.rows(), r.cols(), time.Now().UTC())
	if err != nil {
		r.clearActive(sessionID)
		return err
	}
	_, ch, unsub := host.SubscribeLive(ctx, pty.DefaultSubscriberBuffer)
	go func() {
		defer r.clearActive(sessionID)
		defer unsub()
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case p, ok := <-ch:
				if !ok {
					return
				}
				_ = w.writePTY(time.Now().UTC(), p)
			}
		}
	}()
	return nil
}

func (r *Recorder) clearActive(sessionID string) {
	r.mu.Lock()
	delete(r.active, sessionID)
	r.mu.Unlock()
}

func (r *Recorder) maxBytes() int64 {
	if r.MaxBytes <= 0 {
		return DefaultRecordingMaxBytes
	}
	return r.MaxBytes
}

func (r *Recorder) rows() uint16 {
	if r.Rows == 0 {
		return pty.DefaultRows
	}
	return r.Rows
}

func (r *Recorder) cols() uint16 {
	if r.Cols == 0 {
		return pty.DefaultCols
	}
	return r.Cols
}

type castWriter struct {
	path      string
	sessionID string
	maxBytes  int64
	rows      uint16
	cols      uint16
	started   time.Time
	index     int
	written   int64
	file      *os.File
	buf       *bufio.Writer
}

func newCastWriter(path, sessionID string, maxBytes int64, rows, cols uint16, started time.Time) (*castWriter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("recording path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	c := &castWriter{
		path:      path,
		sessionID: sessionID,
		maxBytes:  maxBytes,
		rows:      rows,
		cols:      cols,
		started:   started,
	}
	if c.started.IsZero() {
		c.started = time.Now().UTC()
	}
	if err := c.open(true); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *castWriter) writePTY(at time.Time, p []byte) error {
	if len(p) == 0 {
		return nil
	}
	if rows, cols, ok := pty.ParseResizeFrame(p); ok {
		return c.writeEvent(at, "r", fmt.Sprintf("%dx%d", cols, rows))
	}
	return c.writeEvent(at, "o", string(p))
}

func (c *castWriter) writeEvent(at time.Time, code, data string) error {
	elapsed := at.Sub(c.started).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return c.writeJSON([]any{elapsed, code, data})
}

func (c *castWriter) open(truncate bool) error {
	flag := os.O_CREATE | os.O_WRONLY
	if truncate {
		flag |= os.O_TRUNC
	} else {
		flag |= os.O_APPEND
	}
	f, err := os.OpenFile(c.path, flag, 0o600)
	if err != nil {
		return err
	}
	c.file = f
	c.buf = bufio.NewWriter(f)
	c.written = 0
	return c.writeHeader()
}

func (c *castWriter) writeHeader() error {
	return c.writeJSON(map[string]any{
		"version":   2,
		"width":     c.cols,
		"height":    c.rows,
		"timestamp": c.started.Unix(),
		"title":     c.sessionID,
	})
}

func (c *castWriter) writeJSON(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if c.maxBytes > 0 && c.written > 0 && c.written+int64(len(line)) > c.maxBytes {
		if err := c.rotate(); err != nil {
			return err
		}
	}
	n, err := c.buf.Write(line)
	c.written += int64(n)
	if err != nil {
		return err
	}
	return c.buf.Flush()
}

func (c *castWriter) rotate() error {
	if err := c.closeFile(); err != nil {
		return err
	}
	c.index++
	rotated := strings.TrimSuffix(c.path, ".cast") + fmt.Sprintf("-%d.cast", time.Now().UTC().UnixNano())
	if c.index > 1 {
		rotated = strings.TrimSuffix(c.path, ".cast") + fmt.Sprintf("-%d-%d.cast", time.Now().UTC().UnixNano(), c.index)
	}
	if err := os.Rename(c.path, rotated); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return c.open(true)
}

func (c *castWriter) Close() error {
	return c.closeFile()
}

func (c *castWriter) closeFile() error {
	if c.buf != nil {
		if err := c.buf.Flush(); err != nil {
			_ = c.file.Close()
			return err
		}
	}
	if c.file == nil {
		return nil
	}
	err := c.file.Close()
	c.file = nil
	c.buf = nil
	return err
}

func safeRecordingName(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "session"
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, sessionID)
}
