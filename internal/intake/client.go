package intake

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"time"
)

func Request(socketPath string, ev Event, timeout time.Duration) (Response, error) {
	if socketPath == "" {
		return Response{}, errors.New("intake: empty socket path")
	}
	if ev.Type != TypeApprovalRequest && ev.Type != TypeClaudeQuestion && !isRPCType(ev.Type) {
		return Response{}, errors.New("intake: unsupported request type")
	}
	c, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return Response{}, err
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	raw, err := json.Marshal(ev)
	if err != nil {
		return Response{}, err
	}
	if _, err := c.Write(append(raw, '\n')); err != nil {
		return Response{}, err
	}
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	var response Response
	if err := json.NewDecoder(bufio.NewReader(c)).Decode(&response); err != nil {
		return Response{}, err
	}
	return response, nil
}
