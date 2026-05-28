package runner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// LoadFromFile reads commands from path, one per line. Lines starting with
// '#' and blank lines are skipped. The returned Commands carry the
// supplied Source.
func LoadFromFile(path string, src Source) ([]Spec, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied path; not a security boundary
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return readLines(f, src)
}

// LoadFromReader reads commands from r, one per line. Same skipping rules
// as LoadFromFile.
func LoadFromReader(r io.Reader, src Source) ([]Spec, error) {
	return readLines(r, src)
}

func readLines(r io.Reader, src Source) ([]Spec, error) {
	var out []Spec
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, Spec{Text: line, Source: src})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
