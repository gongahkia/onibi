package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/envelope"
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

type ptyResizeFrame struct {
	Type string `json:"type"`
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

func (s *Server) handleWSPTY(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireWSAuth(w, r)
	if !ok {
		return
	}
	codec, err := s.e2eCodec(ownerSessionID, e2eInfoPTY)
	if err != nil {
		s.log.Warn("web pty e2e unavailable", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ptySubprotocol}})
	if err != nil {
		s.log.Warn("web pty ws accept failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		return
	}
	defer c.CloseNow()
	reqID := requestID(r)
	var sessionID string
	var bytesIn atomic.Uint64
	var bytesOut atomic.Uint64
	s.log.Info("web pty ws accepted", "request_id", reqID, "remote", remoteHost(r.RemoteAddr))
	defer func() {
		s.log.Info("web pty ws closed",
			"request_id", reqID,
			"session_id", sessionID,
			"remote", remoteHost(r.RemoteAddr),
			"duration_ms", time.Since(started).Milliseconds(),
			"bytes_from_client", bytesIn.Load(),
			"bytes_to_client", bytesOut.Load(),
		)
	}()
	c.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.pingLoop(ctx, c)

	attach, err := readPTYAttach(ctx, c, codec)
	if err != nil {
		s.log.Warn("web pty attach failed", "request_id", reqID, "err", err, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		_ = c.Close(websocket.StatusPolicyViolation, "attach required")
		return
	}
	sessionID = attach.SessionID
	host, ok := s.hostForSession(ctx, attach.SessionID)
	if !ok {
		s.log.Warn("web pty attach failed", "request_id", reqID, "reason", "unknown_session", "session_id", attach.SessionID, "remote", remoteHost(r.RemoteAddr), "duration_ms", time.Since(started).Milliseconds())
		_ = c.Close(websocket.StatusPolicyViolation, "unknown session")
		return
	}
	s.log.Info("web pty attached", "request_id", reqID, "session_id", attach.SessionID, "last_seq", attach.LastSeq, "attach_latency_ms", time.Since(started).Milliseconds())

	replay, ch, unsub := host.SubscribeFrom(ctx, pty.DefaultSubscriberBuffer, attach.LastSeq)
	defer unsub()
	var writeMu sync.Mutex
	if replay.Snapshot {
		bytesOut.Add(uint64(len(replay.Data)))
		err = writeWSJSON(ctx, c, &writeMu, ptySnapshotFrame{
			Type:       "snapshot",
			Seq:        replay.Seq,
			Base64Data: base64.StdEncoding.EncodeToString(replay.Data),
		}, codec)
	} else if len(replay.Data) > 0 {
		bytesOut.Add(uint64(len(replay.Data)))
		err = writeWSBinary(ctx, c, &writeMu, replay.Data, codec)
	}
	if err != nil {
		s.log.Warn("web pty replay failed", "request_id", reqID, "session_id", attach.SessionID, "err", err, "snapshot", replay.Snapshot, "replay_bytes", len(replay.Data))
		return
	}
	s.log.Info("web pty replay sent", "request_id", reqID, "session_id", attach.SessionID, "snapshot", replay.Snapshot, "replay_bytes", len(replay.Data), "seq", replay.Seq)

	go func() {
		for p := range ch {
			bytesOut.Add(uint64(len(p)))
			if rows, cols, ok := pty.ParseResizeFrame(p); ok {
				err := writeWSJSON(ctx, c, &writeMu, ptyResizeFrame{Type: "resize", Rows: rows, Cols: cols}, codec)
				if err != nil {
					cancel()
					return
				}
			} else {
				if err := writeWSBinary(ctx, c, &writeMu, p, codec); err != nil {
					cancel()
					return
				}
			}
		}
		_ = c.Close(websocket.StatusNormalClosure, "pty closed")
		cancel()
	}()

	for {
		typ, p, err := c.Read(ctx)
		if err != nil {
			s.log.Debug("web pty read ended", "request_id", reqID, "session_id", attach.SessionID, "err", err, "duration_ms", time.Since(started).Milliseconds())
			return
		}
		bytesIn.Add(uint64(len(p)))
		if err := handlePTYClientFrame(host, typ, p, codec); err != nil {
			s.log.Warn("web pty client frame rejected", "request_id", reqID, "session_id", attach.SessionID, "message_type", typ.String(), "bytes", len(p), "err", err)
			_ = c.Close(websocket.StatusPolicyViolation, "invalid frame")
			return
		}
	}
}

func readPTYAttach(ctx context.Context, c *websocket.Conn, codec *envelope.Codec) (ptyAttachFrame, error) {
	readCtx, cancel := context.WithTimeout(ctx, ptyAttachTimeout)
	defer cancel()
	typ, p, err := c.Read(readCtx)
	if err != nil {
		return ptyAttachFrame{}, err
	}
	typ, p, err = wsDecrypt(codec, typ, p)
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

func handlePTYClientFrame(host *pty.Host, typ websocket.MessageType, p []byte, codec *envelope.Codec) error {
	var err error
	typ, p, err = wsDecrypt(codec, typ, p)
	if err != nil {
		return err
	}
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

func writeWSJSON(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, v any, codec *envelope.Codec) error {
	p, err := json.Marshal(v)
	if err != nil {
		return err
	}
	typ, p, err := wsEncrypt(codec, websocket.MessageText, p)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	mu.Lock()
	defer mu.Unlock()
	if codec == nil {
		return wsjson.Write(writeCtx, c, v)
	}
	return c.Write(writeCtx, typ, p)
}

func writeWSBinary(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, p []byte, codec *envelope.Codec) error {
	typ, p, err := wsEncrypt(codec, websocket.MessageBinary, p)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	mu.Lock()
	defer mu.Unlock()
	return c.Write(writeCtx, typ, p)
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
