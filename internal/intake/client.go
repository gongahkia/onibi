package intake

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"time"
)

// Send writes ev as JSON to the daemon's intake socket. Caller-side fail-
// open contract: if the socket isn't there, the daemon is down, or any
// network error occurs, return that error — callers (onibi-notify) should
// exit 0 silently regardless.
func Send(socketPath string, ev Event) error {
	if socketPath == "" {
		return errors.New("intake: empty socket path")
	}
	c, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = c.Write(append(b, '\n'))
	return err
}

// Request writes ev (must be an approval_request) and blocks reading a
// Response. Caller supplies an overall timeout (typically the approval TTL
// + a few seconds slack). Returns the daemon's decision or an error.
func Request(socketPath string, ev Event, timeout time.Duration) (Response, error) {
	if socketPath == "" {
		return Response{}, errors.New("intake: empty socket path")
	}
	if ev.Type != TypeApprovalRequest {
		return Response{}, errors.New("intake: Request only valid for approval_request")
	}
	c, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	b, err := json.Marshal(ev)
	if err != nil {
		return Response{}, err
	}
	if _, err := c.Write(append(b, '\n')); err != nil {
		return Response{}, err
	}
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	dec := json.NewDecoder(bufio.NewReader(c))
	var r Response
	if err := dec.Decode(&r); err != nil {
		return Response{}, err
	}
	return r, nil
}
