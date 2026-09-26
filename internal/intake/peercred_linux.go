//go:build linux

package intake

import "golang.org/x/sys/unix"

// readPeerUID returns the uid of the connecting peer on Linux via
// getsockopt(SOL_SOCKET, SO_PEERCRED).
func readPeerUID(fd int) (uint32, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
