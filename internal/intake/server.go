package intake

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Handler processes a validated event. Implementations should be cheap
// (dispatch to a worker queue if heavy). Returning an error logs the event
// but does not surface to the hook (fail-open contract).
type Handler func(context.Context, Event) error

// Server listens on a Unix domain socket for JSON events from hooks.
type Server struct {
	socketPath string
	handler    Handler
	logger     *slog.Logger
	ln         net.Listener
}

// New returns a Server bound to socketPath. Call Serve to begin accepting.
// The socket file is created on Serve with 0600 perms.
func New(socketPath string, handler Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{socketPath: socketPath, handler: handler, logger: logger}
}

// Serve binds the socket and serves until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return fmt.Errorf("intake mkdir: %w", err)
	}
	// remove stale socket left by an unclean exit
	_ = os.Remove(s.socketPath)

	// umask-resistant create: bind, then chmod
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("intake listen: %w", err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("intake chmod: %w", err)
	}
	s.ln = ln

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	defer os.Remove(s.socketPath)

	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logger.Warn("intake accept", slog.Any("err", err))
			continue
		}
		go s.handle(ctx, c)
	}
}

// SocketPath returns the path the server is bound to.
func (s *Server) SocketPath() string { return s.socketPath }

func (s *Server) handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	// peer-credential check: the connecting process must run as the same
	// uid as us. Refuses cross-user intake on shared hosts (rare for a
	// laptop but cheap defense in depth — TODO §7.3, threat T11).
	if err := verifyPeerUID(c); err != nil {
		s.logger.Warn("intake peer rejected", slog.Any("err", err))
		return
	}

	dec := json.NewDecoder(bufio.NewReader(c))
	dec.DisallowUnknownFields()

	var ev Event
	if err := dec.Decode(&ev); err != nil {
		if !errors.Is(err, io.EOF) {
			s.logger.Warn("intake bad payload", slog.Any("err", err))
		}
		return
	}
	if ev.Type == "" {
		s.logger.Warn("intake missing type")
		return
	}
	if ev.TS == 0 {
		ev.TS = time.Now().Unix()
	}
	if err := s.handler(ctx, ev); err != nil {
		s.logger.Warn("intake handler", slog.String("type", ev.Type), slog.Any("err", err))
	}
}

// verifyPeerUID uses platform-specific APIs to read the connecting peer's
// effective uid and rejects if it differs from ours.
func verifyPeerUID(c net.Conn) error {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return errors.New("not unix conn")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var peerUID uint32
	var inner error
	err = raw.Control(func(fd uintptr) {
		peerUID, inner = readPeerUID(int(fd))
	})
	if err != nil {
		return err
	}
	if inner != nil {
		return inner
	}
	myUID := uint32(syscall.Geteuid())
	if peerUID != myUID {
		return fmt.Errorf("uid mismatch: peer=%d self=%d", peerUID, myUID)
	}
	return nil
}
