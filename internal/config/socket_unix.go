//go:build unix

package config

import (
	"io/fs"
	"syscall"
)

func checkOwner(info fs.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(st.Uid) == uid
}
