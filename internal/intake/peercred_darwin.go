//go:build darwin

package intake

import "syscall"

// readPeerUID returns the effective uid of the connecting peer on macOS.
// Uses SOL_LOCAL / LOCAL_PEEREPID? — actually LOCAL_PEEREUID for euid;
// stdlib exposes GetsockoptInt with these values.
func readPeerUID(fd int) (uint32, error) {
	uid, err := syscall.GetsockoptInt(fd, syscall.SOL_LOCAL, syscall.LOCAL_PEERUID)
	if err != nil {
		return 0, err
	}
	return uint32(uid), nil
}
