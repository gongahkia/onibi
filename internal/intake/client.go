package intake

import (
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
