package logwriter

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// fileTimeLayout formats the local-time timestamp embedded in the per-run
// directory name and in each per-command file name. See contracts/logging.md.
const fileTimeLayout = "20060102-150405"

// maxSlugLen bounds the slug portion of a file name so the assembled name
// stays well under the 255-byte POSIX NAME_MAX. The full name is
// timestamp(15) + "_" + slug + "_" + id(8) + ".log"(4); bounding the slug at
// 80 leaves ample margin.
const maxSlugLen = 80

// idBytes is the number of random bytes in a per-command id. Hex-encoded it
// yields idBytes*2 characters (8 hex chars).
const idBytes = 4

// runRandBytes is the number of random bytes in the per-run directory suffix
// (4 hex chars), enough to keep two simultaneous runners' directories
// distinct.
const runRandBytes = 2

// FileName builds the per-command log file name:
//
//	<timestamp>_<slug>_<id>.log
//
// timestamp is start rendered in local time, slug is the sanitized command
// text, and id is a short random hex token guaranteeing uniqueness. The id is
// supplied by the caller (see Run.NewRecord) so name construction stays pure
// and testable.
func FileName(start time.Time, text, id string) string {
	return start.Format(fileTimeLayout) + "_" + Slug(text) + "_" + id + ".log"
}

// Slug renders command text as a lowercase, hyphen-separated, filesystem-safe
// token derived from the full command. Runs of characters outside [a-z0-9]
// collapse to a single '-'; leading/trailing hyphens are trimmed; an empty
// result becomes "cmd"; the result is truncated to maxSlugLen. Because the
// sanitized output is pure ASCII, truncation is rune-safe.
func Slug(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	prevHyphen := false
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	if s == "" {
		return "cmd"
	}
	return s
}

// randomID returns the per-command file-name id: a short random lowercase hex
// token (idBytes*2 chars) that guarantees uniqueness across parallel, repeated,
// and two-runner executions without any shared counter.
func randomID() (string, error) {
	return randomHex(idBytes)
}

// randomHex returns n random bytes hex-encoded (2n lowercase chars).
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
