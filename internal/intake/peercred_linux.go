//go:build linux

package intake

import "syscall"

// readPeerUID returns the effective uid of the connecting peer on Linux
// via SO_PEERCRED. Returns the credentials struct's Uid field.
func readPeerUID(fd int) (uint32, error) {
	cred, err := syscall.GetsockoptUcred(fd, syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return cred.Uid, nil
}
