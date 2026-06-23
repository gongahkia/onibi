package web

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (s *Server) handleWSEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionID, ok := s.requireWSAuth(w, r)
	if !ok {
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{eventsSubprotocol}})
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
		"endpoint":   "events",
		"session_id": sessionID,
	})
	_ = c.Close(websocket.StatusNormalClosure, "ok")
}
