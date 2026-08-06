package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gongahkia/onibi/internal/render"
)

const screenFrameTTL = 30 * time.Second
const screenFrameLimit = 32

type terminalFrame struct {
	PNG         []byte
	Tail        string
	Fingerprint string
	CapturedAt  time.Time
}

func (d *Daemon) CaptureSessionFrame(ctx context.Context, id string) (terminalFrame, error) {
	s, err := d.sessionForRPCTarget(id)
	if err != nil {
		return terminalFrame{}, err
	}
	rows, cols := 26, 100
	var raw []byte
	if s.Transport == "tmux" {
		ctrl := newTmuxController()
		cols, rows = tmuxScreenDimensions(ctx, ctrl, s.TmuxTarget)
		out, err := ctrl.Capture(ctx, s.TmuxTarget, maxInt(rows*3, 160))
		if err != nil {
			return terminalFrame{}, d.tmuxSessionError(ctx, s, err)
		}
		raw = []byte(out)
		s.Buf.Reset()
		_, _ = s.Buf.Write(raw)
		d.touchSession(ctx, s)
	} else {
		raw = s.Buf.Snapshot()
		rows, cols = outputScreenDimensions(raw)
	}
	opts := d.screenPNGOptions(rows, cols)
	fingerprint := screenFingerprint(raw, rows, cols, opts)
	d.frameMu.Lock()
	cached, ok := d.frames[s.ID]
	d.frameMu.Unlock()
	if ok && cached.Fingerprint == fingerprint && time.Since(cached.CapturedAt) < screenFrameTTL {
		return cloneFrame(cached), nil
	}
	png, err := render.RenderPNG(raw, opts)
	if err != nil {
		return terminalFrame{}, err
	}
	frame := terminalFrame{PNG: png, Tail: render.TextTailBody(raw, render.Options{MaxLines: 80, MaxChars: 3500}), Fingerprint: fingerprint, CapturedAt: time.Now()}
	d.frameMu.Lock()
	if _, exists := d.frames[s.ID]; !exists && len(d.frames) >= screenFrameLimit {
		var oldestID string
		var oldest time.Time
		for id, candidate := range d.frames {
			if oldestID == "" || candidate.CapturedAt.Before(oldest) {
				oldestID, oldest = id, candidate.CapturedAt
			}
		}
		delete(d.frames, oldestID)
	}
	d.frames[s.ID] = cloneFrame(frame)
	d.frameMu.Unlock()
	return frame, nil
}

func (d *Daemon) CachedSessionFrame(id string) (terminalFrame, bool) {
	d.frameMu.Lock()
	defer d.frameMu.Unlock()
	frame, ok := d.frames[id]
	if !ok || time.Since(frame.CapturedAt) >= screenFrameTTL {
		delete(d.frames, id)
		return terminalFrame{}, false
	}
	return cloneFrame(frame), true
}

func (d *Daemon) InvalidateSessionFrame(id string) {
	d.frameMu.Lock()
	delete(d.frames, id)
	d.frameMu.Unlock()
}

func (d *Daemon) InvalidateAllSessionFrames() {
	d.frameMu.Lock()
	d.frames = map[string]terminalFrame{}
	d.frameMu.Unlock()
}

func cloneFrame(frame terminalFrame) terminalFrame {
	frame.PNG = append([]byte(nil), frame.PNG...)
	return frame
}

func screenFingerprint(raw []byte, rows, cols int, opts render.PNGOptions) string {
	h := sha256.New()
	_, _ = h.Write(raw)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(opts.Font + "\x00" + opts.FontPath + "\x00" + strconv.Itoa(rows) + "x" + strconv.Itoa(cols)))
	return hex.EncodeToString(h.Sum(nil))
}

func outputScreenDimensions(raw []byte) (int, int) {
	lines := strings.Split(string(render.StripANSI(raw)), "\n")
	rows, cols := 16, 40
	for _, line := range lines {
		if width := utf8.RuneCountInString(line); width > cols {
			cols = width
		}
	}
	if len(lines) > rows {
		rows = len(lines)
	}
	if rows > 60 {
		rows = 60
	}
	if cols > 140 {
		cols = 140
	}
	return rows, cols
}
