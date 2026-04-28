// Package shareddir holds helpers for the ./shared/ collaboration directory:
// gitignore-style path filtering, size caps, and path-traversal validation.
package shareddir

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// MaxFileBytes caps the size of any file we sync. Mirrors the on-the-wire
// limit in pkg/protocol (256 KB).
const MaxFileBytes = 256 * 1024

// Hard-coded paths we never sync, regardless of .gitignore. Editor + tooling
// noise that would only generate churn.
var defaultSkipPrefixes = []string{
	".git/",
	".venv/",
	"venv/",
	"__pycache__/",
	"node_modules/",
	"dist/",
	"build/",
	"target/",
}

var defaultSkipSuffixes = []string{
	".lock",
}

// Filter decides whether a relative path inside the shared dir should be
// synced. Read once at startup; not designed to track .gitignore changes
// during a session.
type Filter struct {
	prefixes []string
	suffixes []string
}

// NewFilter loads the defaults plus a best-effort parse of <sharedDir>/.gitignore.
func NewFilter(sharedDir string) *Filter {
	f := &Filter{
		prefixes: append([]string(nil), defaultSkipPrefixes...),
		suffixes: append([]string(nil), defaultSkipSuffixes...),
	}
	if file, err := os.Open(filepath.Join(sharedDir, ".gitignore")); err == nil {
		defer file.Close()
		s := bufio.NewScanner(file)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
				continue
			}
			line = strings.TrimPrefix(line, "/")
			switch {
			case strings.HasSuffix(line, "/"):
				f.prefixes = append(f.prefixes, line)
			case strings.HasPrefix(line, "*."):
				f.suffixes = append(f.suffixes, line[1:])
			default:
				f.prefixes = append(f.prefixes, line)
			}
		}
	}
	return f
}

// ShouldSkip returns true if path (relative, forward-slashed) should be
// ignored by the sync engine.
func (f *Filter) ShouldSkip(path string) bool {
	path = filepath.ToSlash(path)
	for _, p := range f.prefixes {
		if path == strings.TrimSuffix(p, "/") || strings.HasPrefix(path, p) {
			return true
		}
	}
	for _, s := range f.suffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

// ValidatePath ensures a wire-supplied path is safe to use as a relative
// path inside the shared dir: non-empty, not absolute, no `..` segments.
func ValidatePath(p string) error {
	if p == "" {
		return errors.New("path is empty")
	}
	if filepath.IsAbs(p) {
		return errors.New("path must be relative")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return errors.New("path must be relative")
	}
	for part := range strings.SplitSeq(filepath.ToSlash(p), "/") {
		if part == ".." {
			return errors.New("path contains '..'")
		}
	}
	return nil
}
