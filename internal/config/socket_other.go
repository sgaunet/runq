//go:build !unix

package config

import "io/fs"

func checkOwner(_ fs.FileInfo, _ int) bool {
	// Non-Unix platforms are not supported by runq (see plan.md). Refuse to
	// trust XDG_RUNTIME_DIR ownership and fall back to the /tmp path.
	return false
}
