package web

import (
	"context"
	"fmt"
	"log/slog"
)

func (s *Server) auditViewerEvent(ctx context.Context, action, sessionID, viewerID, remote, userAgent string) {
	if s.db == nil {
		return
	}
	detail := fmt.Sprintf("viewer_id=%s remote=%s user_agent=%q", viewerID, remote, userAgent)
	if err := s.db.AuditAppend(ctx, action, sessionID, "", 0, detail); err != nil {
		s.log.Warn("audit append", slog.String("action", action), slog.Any("err", err))
	}
}
