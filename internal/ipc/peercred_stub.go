//go:build !linux && !darwin && !freebsd

package ipc

import (
	"errors"
	"net"
)

// peerUID is not supported on this platform; runq does not support
// non-Unix targets in v1 (see plan.md).
func peerUID(_ *net.UnixConn) (int, error) {
	return -1, errors.New("peer credentials are not supported on this platform")
}
