//go:build darwin || freebsd

package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID returns the kernel-attested uid of the connecting peer on
// macOS / FreeBSD via getpeereid (via the unix package wrapper).
func peerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, err
	}
	var uid uint32
	var ctlErr error
	err = raw.Control(func(fd uintptr) {
		var euid, egid uint32
		euid, egid, ctlErr = getpeereid(int(fd))
		_ = egid
		uid = euid
	})
	if err != nil {
		return -1, err
	}
	if ctlErr != nil {
		return -1, fmt.Errorf("getpeereid: %w", ctlErr)
	}
	return int(uid), nil
}

func getpeereid(fd int) (euid, egid uint32, err error) {
	// LOCAL_PEERCRED is available; but x/sys provides getpeereid via its
	// own helper. We call the syscall directly with the unix package's
	// stable wrapper if available; fall back to manual getsockopt.
	xucred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, 0, err
	}
	return xucred.Uid, 0, nil
}
