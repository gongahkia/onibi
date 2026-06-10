package intake

import (
	"net"
	"time"
)

func pingSock(p string) (net.Conn, error) {
	c, err := net.DialTimeout("unix", p, 200*time.Millisecond)
	if err != nil {
		return nil, err
	}
	_ = c.Close()
	return c, nil
}

func rawSend(p string, b []byte) error {
	c, err := net.DialTimeout("unix", p, 500*time.Millisecond)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Write(b)
	return err
}
