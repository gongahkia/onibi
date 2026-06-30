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
	"sync"
	"syscall"
	"time"
)

// Handler processes a validated fire-and-forget event. Implementations
// should be cheap (dispatch to a worker queue if heavy). Returning an error
// logs the event but does not surface to the hook (fail-open contract).
type Handler func(context.Context, Event) error

// ApprovalHandler processes an approval_request event in RPC mode: returns
// the Response the server should write back to the still-open client conn.
// Implementations may block for up to the approval TTL waiting for a
// decision. Return error to indicate transient failure (the server will
// write a Cancelled response so the hook unblocks).
type ApprovalHandler func(context.Context, Event) (Response, error)

type RPCHandler func(context.Context, Event) (Response, error)

var readPeerUIDMu sync.RWMutex
var readPeerUIDFunc = readPeerUID

// Server listens on a Unix domain socket for JSON events from hooks.
type Server struct {
	socketPath string
	handler    Handler
	approval   ApprovalHandler
	rpc        RPCHandler
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

// SetApprovalHandler installs the RPC-mode handler used for approval_request
// events. Without one, approval_request payloads are rejected with a
// cancelled response (matches fail-open spirit on the daemon side: the
// agent's tool call is blocked but the hook unblocks promptly).
func (s *Server) SetApprovalHandler(h ApprovalHandler) { s.approval = h }

func (s *Server) SetRPCHandler(h RPCHandler) { s.rpc = h }

// Serve binds the socket and serves until ctx is cancelled.
func (s *Server) Serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return fmt.Errorf("intake mkdir: %w", err)
	}
	if SocketActive(s.socketPath, 200*time.Millisecond) {
		return fmt.Errorf("intake socket already in use: %s", s.socketPath)
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

func SocketActive(socketPath string, timeout time.Duration) bool {
	if socketPath == "" {
		return false
	}
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	c, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func (s *Server) handle(ctx context.Context, c net.Conn) {
	defer c.Close()

	// peer-credential check: the connecting process must run as the same
	// uid as us. Refuses cross-user intake on shared hosts (rare for a
	// laptop but cheap defense in depth).
	if err := verifyPeerUID(c); err != nil {
		s.logger.Warn("intake peer rejected", slog.Any("err", err))
		return
	}

	// short deadline for the first read — payloads are JSON one-liners
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))

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

	// approval_request is RPC: hold the conn open while the daemon waits
	// for a web decision. Disable the read deadline; set a long write
	// deadline for when we eventually send the response.
	if ev.Type == TypeApprovalRequest {
		s.handleApproval(ctx, c, ev)
		return
	}
	if isRPCType(ev.Type) {
		s.handleRPC(ctx, c, ev)
		return
	}

	// fire-and-forget event
	if err := s.handler(ctx, ev); err != nil {
		s.logger.Warn("intake handler", slog.String("type", ev.Type), slog.Any("err", err))
	}
}

func (s *Server) handleRPC(ctx context.Context, c net.Conn, ev Event) {
	if s.rpc == nil {
		_ = writeResponse(c, Response{Reason: "daemon has no rpc handler"})
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	resp, err := s.rpc(ctx, ev)
	if err != nil {
		s.logger.Warn("rpc handler error", slog.String("type", ev.Type), slog.Any("err", err))
		resp = Response{Reason: err.Error()}
	}
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if werr := writeResponse(c, resp); werr != nil {
		s.logger.Warn("rpc write response", slog.Any("err", werr))
	}
}

func (s *Server) handleApproval(ctx context.Context, c net.Conn, ev Event) {
	if s.approval == nil {
		_ = writeResponse(c, Response{Decision: "cancelled", Reason: "daemon has no approval handler"})
		return
	}
	// clear deadlines while we wait for the user (up to approval TTL —
	// daemon's approval queue enforces a hard 5-min ceiling so the conn
	// won't leak)
	_ = c.SetReadDeadline(time.Time{})

	resp, err := s.approval(ctx, ev)
	if err != nil {
		s.logger.Warn("approval handler error", slog.Any("err", err))
		resp = Response{Decision: "cancelled", Reason: "approval handler error"}
	}
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if werr := writeResponse(c, resp); werr != nil {
		s.logger.Warn("approval write response", slog.Any("err", werr))
	}
}

func isRPCType(typ string) bool {
	return typ == TypeSessionInput || typ == TypeSessionPeek || typ == TypeSessionNew || typ == TypeSessionShow || typ == TypeSessionHide || typ == TypeDemoApproval || typ == TypeTrust || typ == TypeBudget || typ == TypeSnapshot || typ == TypePing
}

func writeResponse(c net.Conn, r Response) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = c.Write(append(b, '\n'))
	return err
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
		readPeerUIDMu.RLock()
		fn := readPeerUIDFunc
		readPeerUIDMu.RUnlock()
		peerUID, inner = fn(int(fd))
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
