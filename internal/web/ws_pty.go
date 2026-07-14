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

	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
)

const wsWriteTimeout = 5 * time.Second

var (
	wsPingInterval      = 30 * time.Second
	wsPingTimeout       = 10 * time.Second
	wsAuthCheckInterval = 5 * time.Second
	ptyAttachTimeout    = 10 * time.Second
	wsPingMu            sync.RWMutex
	wsAuthCheckMu       sync.RWMutex
	ptyAttachTimeoutMu  sync.RWMutex
)

type ptyAttachFrame struct {
	Type        string `json:"type"`
	SessionID   string `json:"session_id"`
	LastSeq     uint64 `json:"last_seq"`
	VerifyToken string `json:"verify_token,omitempty"`
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

type ptyRecoveryFrame struct {
	Type        string `json:"type"`
	Mode        string `json:"mode"`
	Seq         uint64 `json:"seq"`
	ReplayBytes int    `json:"replay_bytes"`
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
	auth, ok := s.requireWSAuthInfo(w, r)
	if !ok {
		return
	}
	ownerSessionID := auth.ID
	readOnly := auth.Role == store.PairRoleViewer
	sessionKey, err := s.e2eSessionKey(ownerSessionID)
	if err != nil {
		s.log.Warn("web pty e2e unavailable", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		http.Error(w, "relay e2e unavailable", http.StatusUnauthorized)
		return
	}
	wsE2E := newSeqWSCodec(sessionKey, ownerSessionID, e2eInfoPTY, e2eDirC2S, e2eDirS2C)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ptySubprotocol}})
	if err != nil {
		s.log.Warn("web pty ws accept failed", "request_id", requestID(r), "err", err, "remote", remoteHost(r.RemoteAddr))
		return
	}
	defer c.CloseNow()
	reqID := requestID(r)
	remote := remoteHost(r.RemoteAddr)
	userAgent := trimForLog(r.UserAgent(), 160)
	var sessionID string
	viewerAttached := false
	var bytesIn atomic.Uint64
	var bytesOut atomic.Uint64
	s.log.Info("web pty ws accepted", "request_id", reqID, "remote", remote, "role", auth.Role)
	defer func() {
		s.log.Info("web pty ws closed",
			"request_id", reqID,
			"session_id", sessionID,
			"remote", remote,
			"duration_ms", time.Since(started).Milliseconds(),
			"bytes_from_client", bytesIn.Load(),
			"bytes_to_client", bytesOut.Load(),
		)
		if viewerAttached {
			s.auditViewerEvent(context.Background(), "viewer.detach", sessionID, ownerSessionID, remote, userAgent)
		}
	}()
	c.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.pingLoop(ctx, c)
	go s.watchWebSession(ctx, c, ownerSessionID, cancel, reqID)

	attach, err := readPTYAttach(ctx, c, wsE2E)
	if err != nil {
		s.log.Warn("web pty attach failed", "request_id", reqID, "err", err, "remote", remote, "duration_ms", time.Since(started).Milliseconds())
		_ = c.Close(websocket.StatusPolicyViolation, "attach required")
		return
	}
	if wsE2E != nil {
		if err := s.verifyRelayAttach(ctx, ownerSessionID, attach.VerifyToken); err != nil {
			s.log.Warn("web pty attach failed", "request_id", reqID, "reason", "bad_relay_verifier", "err", err, "remote", remote, "duration_ms", time.Since(started).Milliseconds())
			_ = c.Close(websocket.StatusPolicyViolation, "bad relay verifier")
			return
		}
	}
	sessionID = attach.SessionID
	host, ok := s.hostForSession(ctx, attach.SessionID)
	if !ok {
		s.log.Warn("web pty attach failed", "request_id", reqID, "reason", "unknown_session", "session_id", attach.SessionID, "remote", remote, "duration_ms", time.Since(started).Milliseconds())
		_ = c.Close(websocket.StatusPolicyViolation, "unknown session")
		return
	}
	s.log.Info("web pty attached", "request_id", reqID, "session_id", attach.SessionID, "last_seq", attach.LastSeq, "attach_latency_ms", time.Since(started).Milliseconds())
	if readOnly {
		s.auditViewerEvent(context.Background(), "viewer.attach", sessionID, ownerSessionID, remote, userAgent)
		viewerAttached = true
	}

	replay, ch, unsub := host.SubscribeFrom(ctx, pty.DefaultSubscriberBuffer, attach.LastSeq)
	defer unsub()
	var writeMu sync.Mutex
	if replay.Snapshot {
		bytesOut.Add(uint64(len(replay.Data)))
		err = writeWSJSON(ctx, c, &writeMu, ptySnapshotFrame{
			Type:       "snapshot",
			Seq:        replay.Seq,
			Base64Data: base64.StdEncoding.EncodeToString(replay.Data),
		}, wsE2E)
	} else if len(replay.Data) > 0 {
		bytesOut.Add(uint64(len(replay.Data)))
		err = writeWSBinary(ctx, c, &writeMu, replay.Data, wsE2E)
	}
	if err != nil {
		s.log.Warn("web pty replay failed", "request_id", reqID, "session_id", attach.SessionID, "err", err, "snapshot", replay.Snapshot, "replay_bytes", len(replay.Data))
		return
	}
	mode := "replay"
	if replay.Snapshot {
		mode = "snapshot"
	}
	if err := writeWSJSON(ctx, c, &writeMu, ptyRecoveryFrame{
		Type:        "recovery",
		Mode:        mode,
		Seq:         replay.Seq,
		ReplayBytes: len(replay.Data),
	}, wsE2E); err != nil {
		s.log.Warn("web pty recovery feedback failed", "request_id", reqID, "session_id", attach.SessionID, "err", err, "snapshot", replay.Snapshot, "replay_bytes", len(replay.Data))
		return
	}
	s.log.Info("web pty replay sent", "request_id", reqID, "session_id", attach.SessionID, "snapshot", replay.Snapshot, "replay_bytes", len(replay.Data), "seq", replay.Seq)

	go func() {
		for p := range ch {
			bytesOut.Add(uint64(len(p)))
			if rows, cols, ok := pty.ParseResizeFrame(p); ok {
				err := writeWSJSON(ctx, c, &writeMu, ptyResizeFrame{Type: "resize", Rows: rows, Cols: cols}, wsE2E)
				if err != nil {
					cancel()
					return
				}
			} else {
				if err := writeWSBinary(ctx, c, &writeMu, p, wsE2E); err != nil {
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
		if err := handlePTYClientFrame(host, typ, p, wsE2E, readOnly); err != nil {
			s.log.Warn("web pty client frame rejected", "request_id", reqID, "session_id", attach.SessionID, "message_type", typ.String(), "bytes", len(p), "err", err)
			_ = c.Close(websocket.StatusPolicyViolation, "invalid frame")
			return
		}
	}
}

func readPTYAttach(ctx context.Context, c *websocket.Conn, codec wsCodec) (ptyAttachFrame, error) {
	readCtx, cancel := context.WithTimeout(ctx, websocketAttachTimeout())
	defer cancel()
	typ, p, err := c.Read(readCtx)
	if err != nil {
		return ptyAttachFrame{}, err
	}
	if codec != nil {
		typ, p, err = codec.decrypt(typ, p)
		if err != nil {
			return ptyAttachFrame{}, err
		}
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

func websocketAttachTimeout() time.Duration {
	ptyAttachTimeoutMu.RLock()
	timeout := ptyAttachTimeout
	ptyAttachTimeoutMu.RUnlock()
	return timeout
}

func handlePTYClientFrame(host *pty.Host, typ websocket.MessageType, p []byte, codec wsCodec, readOnly bool) error {
	var err error
	if codec != nil {
		typ, p, err = codec.decrypt(typ, p)
		if err != nil {
			return err
		}
	}
	switch typ {
	case websocket.MessageBinary:
		if readOnly {
			return nil
		}
		_, err := host.Write(p)
		return err
	case websocket.MessageText:
		if frame, ok := parsePTYControlFrame(p); ok {
			if frame.Type == "resize" {
				return host.Resize(frame.Rows, frame.Cols)
			}
			if frame.Type == "attach" {
				return errors.New("web: duplicate attach")
			}
		}
		if readOnly {
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

func writeWSJSON(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, v any, codec wsCodec) error {
	p, err := json.Marshal(v)
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
	typ, p, err := codec.encrypt(websocket.MessageText, p)
	if err != nil {
		return err
	}
	return c.Write(writeCtx, typ, p)
}

func writeWSBinary(ctx context.Context, c *websocket.Conn, mu *sync.Mutex, p []byte, codec wsCodec) error {
	writeCtx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	mu.Lock()
	defer mu.Unlock()
	typ := websocket.MessageBinary
	if codec != nil {
		var err error
		typ, p, err = codec.encrypt(websocket.MessageBinary, p)
		if err != nil {
			return err
		}
	}
	return c.Write(writeCtx, typ, p)
}

func (s *Server) pingLoop(ctx context.Context, c *websocket.Conn) {
	interval, _ := currentWSPingConfig()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, timeout := currentWSPingConfig()
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := c.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "ping timeout")
				return
			}
		}
	}
}

func (s *Server) watchWebSession(ctx context.Context, c *websocket.Conn, sessionID string, cancel context.CancelFunc, reqID string) {
	if s.db == nil {
		return
	}
	interval := currentWSAuthCheckInterval()
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status, err := s.db.WebSessionStatus(ctx, sessionID)
			if err != nil {
				s.log.Warn("web ws session check failed", "request_id", reqID, "session_id", sessionID, "err", err)
				continue
			}
			if status.Valid {
				continue
			}
			reason := status.Reason
			if reason == "" {
				reason = store.WebSessionReasonRevoked
			}
			s.log.Warn("web ws session invalidated", "request_id", reqID, "session_id", sessionID, "reason", reason)
			_ = c.Close(websocket.StatusPolicyViolation, reason)
			cancel()
			return
		}
	}
}

func currentWSPingConfig() (time.Duration, time.Duration) {
	wsPingMu.RLock()
	defer wsPingMu.RUnlock()
	return wsPingInterval, wsPingTimeout
}

func currentWSAuthCheckInterval() time.Duration {
	wsAuthCheckMu.RLock()
	defer wsAuthCheckMu.RUnlock()
	return wsAuthCheckInterval
}
