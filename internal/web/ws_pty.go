package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/pty"
)

const (
	ptyAttachTimeout = 10 * time.Second
	wsWriteTimeout   = 5 * time.Second
)

type ptyAttachFrame struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	LastSeq   uint64 `json:"last_seq"`
}

type ptyControlFrame struct {
	Type string `json:"type"`
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

type ptySnapshotFrame struct {
	Type       string `json:"type"`
	Seq        uint64 `json:"seq"`
	Base64Data string `json:"base64_data"`
}

func (s *Server) handleWSPTY(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, ok := s.requireWSAuth(w, r)
	if !ok {
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ptySubprotocol}})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.pingLoop(ctx, c)

	attach, err := readPTYAttach(ctx, c)
	if err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, "attach required")
		return
	}
	host, ok := s.hostForSession(attach.SessionID)
	if !ok {
		_ = c.Close(websocket.StatusPolicyViolation, "unknown session")
		return
	}

	replay, ch, unsub := host.SubscribeFrom(ctx, pty.DefaultSubscriberBuffer, attach.LastSeq)
	defer unsub()
	var writeMu sync.Mutex
	if replay.Snapshot {
		err = writeWSJSON(ctx, c, &writeMu, ptySnapshotFrame{
			Type:       "snapshot",
			Seq:        replay.Seq,
			Base64Data: base64.StdEncoding.EncodeToString(replay.Data),
		})
	} else if len(replay.Data) > 0 {
		err = writeWSBinary(ctx, c, &writeMu, replay.Data)
	}
	if err != nil {
		return
	}

	go func() {
		for p := range ch {
			if err := writeWSBinary(ctx, c, &writeMu, p); err != nil {
				cancel()
				return
			}
		}
		_ = c.Close(websocket.StatusNormalClosure, "pty closed")
		cancel()
	}()

	for {
		typ, p, err := c.Read(ctx)
		if err != nil {
			return
		}
		if err := handlePTYClientFrame(host, typ, p); err != nil {
			_ = c.Close(websocket.StatusPolicyViolation, "invalid frame")
			return
		}
	}
}

func readPTYAttach(ctx context.Context, c *websocket.Conn) (ptyAttachFrame, error) {
	readCtx, cancel := context.WithTimeout(ctx, ptyAttachTimeout)
	defer cancel()
	typ, p, err := c.Read(readCtx)
	if err != nil {
		return ptyAttachFrame{}, err
	}
	if typ != websocket.MessageText {
		return ptyAttachFrame{}, errors.New("web: attach must be text")
	}
	var attach ptyAttachFrame
	if err := json.Unmarshal(p, &attach); err != nil {
		return ptyAttachFrame{}, err
	}
	if attach.Type != "attach" || attach.SessionID == "" {
		return ptyAttachFrame{}, errors.New("web: invalid attach")
	}
	return attach, nil
}

func handlePTYClientFrame(host *pty.Host, typ websocket.MessageType, p []byte) error {
	switch typ {
	case websocket.MessageBinary:
		_, err := host.Write(p)
		return err
	case websocket.MessageText:
		if frame, ok := parsePTYControlFrame(p); ok {
			if frame.Type == "resize" {
				return host.Resize(frame.Rows, frame.Cols)
			}
			return nil
		}
		_, err := host.Write(p)
		return err
	default:
		return errors.New("web: unsupported ws frame")
	}
}

func parsePTYControlFrame(p []byte) (ptyControlFrame, bool) {
	if len(p) == 0 || p[0] != '{' {
		return ptyControlFrame{}, false
	}
	var frame ptyControlFrame
	if err := json.Unmarshal(p, &frame); err != nil || frame.Type == "" {
		return ptyControlFrame{}, false
	}
	return frame, true
}

func writeWSJSON(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, v any) error {
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	mu.Lock()
	defer mu.Unlock()
	return wsjson.Write(writeCtx, c, v)
}

func writeWSBinary(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, p []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	mu.Lock()
	defer mu.Unlock()
	return c.Write(writeCtx, websocket.MessageBinary, p)
}

func (s *Server) pingLoop(ctx context.Context, c *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "ping timeout")
				return
			}
		}
	}
}
