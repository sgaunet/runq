package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultSocketPath returns the per-user socket path per contracts/cli.md:
//
//   - $XDG_RUNTIME_DIR/runq.sock if $XDG_RUNTIME_DIR is set and the directory
//     exists, is owned by the current uid, and is mode 0700.
//   - /tmp/runq-$UID.sock as a portable fallback.
//
// The function does not create the socket; it only resolves the path.
func DefaultSocketPath() string {
	return defaultSocketPath(os.Getenv, os.Stat, os.Getuid)
}

func defaultSocketPath(
	getenv func(string) string,
	stat func(string) (os.FileInfo, error),
	getuid func() int,
) string {
	uid := getuid()
	if dir := getenv("XDG_RUNTIME_DIR"); dir != "" {
		info, err := stat(dir)
		if err == nil && info.IsDir() && isOwnedMode0700(info, uid) {
			return filepath.Join(dir, "runq.sock")
		}
	}
	return fmt.Sprintf("/tmp/runq-%d.sock", uid)
}

// isOwnedMode0700 returns true when info reports a directory whose mode bits
// (permission portion) are exactly 0700. Ownership is best-effort: on
// platforms where FileInfo.Sys() does not expose uid (e.g., Windows), the
// caller must rely on the mode check alone. Windows is not a supported
// platform for runq anyway (see plan.md), so this is acceptable.
func isOwnedMode0700(info fs.FileInfo, uid int) bool {
	if info.Mode().Perm() != 0o700 {
		return false
	}
	return checkOwner(info, uid)
}
