// Package webadmin exposes pairing and device administration only to the local
// Unix user. This channel never accepts terminal commands or serves HTTP.
package webadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/web"
)

type Admin interface {
	FreshPairingURL() (string, error)
	ListDevices(context.Context) ([]web.Device, error)
	RevokeDevice(context.Context, string) error
	RevokeAllDevices(context.Context) error
	RuntimeStatus() web.RuntimeStatus
}
type Request struct {
	Operation string `json:"operation"`
	ID        string `json:"id,omitempty"`
}
type Response struct {
	PairingURL string             `json:"pairingURL,omitempty"`
	Devices    []web.Device       `json:"devices,omitempty"`
	Status     *web.RuntimeStatus `json:"status,omitempty"`
	Error      string             `json:"error,omitempty"`
}
type Server struct {
	listener *net.UnixListener
	done     chan struct{}
	wg       sync.WaitGroup
}

func Path(stateDir string) string { return filepath.Join(stateDir, "web-admin.sock") }

// Listen requires the caller's exclusive web-state lock. It only removes stale
// socket files, never arbitrary files or links. The directory must be private.
func Listen(path string, admin Admin) (*Server, error) {
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("web administration requires a private state directory")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace non-socket administration path")
		}
		if c, err := net.DialTimeout("unix", path, 100*time.Millisecond); err == nil {
			c.Close()
			return nil, errors.New("web administration socket is already active")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	s := &Server{listener: listener, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		for {
			c, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			s.wg.Add(1)
			go func() { defer s.wg.Done(); defer c.Close(); serve(c, admin) }()
		}
	}()
	return s, nil
}
func (s *Server) Close() error { err := s.listener.Close(); <-s.done; s.wg.Wait(); return err }
func sameUser(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var peerErr error
	if err = raw.Control(func(fd uintptr) { uid, peerErr = readPeerUID(int(fd)) }); err != nil {
		return err
	}
	if peerErr != nil {
		return peerErr
	}
	if uid != uint32(os.Getuid()) {
		return errors.New("administration peer is not the current user")
	}
	return nil
}
func serve(c *net.UnixConn, admin Admin) {
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if sameUser(c) != nil {
		return
	}
	var request Request
	decoder := json.NewDecoder(io.LimitReader(c, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var response Response
	var err error
	switch request.Operation {
	case "status":
		status := admin.RuntimeStatus()
		response.Status = &status
	case "pair":
		response.PairingURL, err = admin.FreshPairingURL()
	case "devices":
		response.Devices, err = admin.ListDevices(ctx)
	case "revoke-all":
		err = admin.RevokeAllDevices(ctx)
	case "revoke":
		err = admin.RevokeDevice(ctx, request.ID)
	default:
		err = errors.New("unknown administration operation")
	}
	if err != nil {
		response = Response{Error: "Local administration failed: " + err.Error()}
	}
	_ = json.NewEncoder(c).Encode(response)
}
func Call(ctx context.Context, path string, request Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return Response{}, fmt.Errorf("connect to Onibi web: start onibi web or onibi web service start first: %w", err)
	}
	defer c.Close()
	if err = sameUser(c.(*net.UnixConn)); err != nil {
		return Response{}, err
	}
	deadline, _ := ctx.Deadline()
	_ = c.SetDeadline(deadline)
	if err = json.NewEncoder(c).Encode(request); err != nil {
		return Response{}, err
	}
	var response Response
	if err = json.NewDecoder(io.LimitReader(c, 128<<10)).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return Response{}, errors.New(response.Error)
	}
	return response, nil
}
