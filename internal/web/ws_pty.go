package web

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (s *Server) handleWSPTY(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionID, ok := s.requireWSAuth(w, r)
	if !ok {
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{ptySubprotocol}})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.pingLoop(ctx, c)
	writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
	defer writeCancel()
	_ = wsjson.Write(writeCtx, c, map[string]any{
		"type":       "server-hello",
		"endpoint":   "pty",
		"session_id": sessionID,
	})
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
	}
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
