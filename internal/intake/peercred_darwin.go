//go:build darwin

package intake

import "golang.org/x/sys/unix"

// readPeerUID returns the uid of the connecting peer on macOS via
// getsockopt(SOL_LOCAL, LOCAL_PEERCRED), which returns a Xucred struct.
func readPeerUID(fd int) (uint32, error) {
	xc, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, err
	}
	return xc.Uid, nil
}
