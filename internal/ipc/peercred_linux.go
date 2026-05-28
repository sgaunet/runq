//go:build linux

package ipc

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID returns the kernel-attested uid of the connecting peer on
// Linux, using SO_PEERCRED.
func peerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, err
	}
	var ucred *unix.Ucred
	var ctlErr error
	err = raw.Control(func(fd uintptr) {
		ucred, ctlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil {
		return -1, err
	}
	if ctlErr != nil {
		return -1, fmt.Errorf("SO_PEERCRED: %w", ctlErr)
	}
	return int(ucred.Uid), nil
}
