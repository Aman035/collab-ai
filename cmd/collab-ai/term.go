package main

import (
	"os"
)

// palette wraps strings in ANSI escapes only when the target file is a TTY
// and NO_COLOR is unset. When color is off, every method returns the input
// unchanged.
type palette struct {
	on bool
}

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiAmber = "\x1b[38;5;215m" // 256-color sodium amber, matches the landing page
)

// newPalette returns a palette gated on f being a terminal.
func newPalette(f *os.File) *palette {
	return &palette{on: isTTY(f) && os.Getenv("NO_COLOR") == ""}
}

func (p *palette) wrap(prefix, s string) string {
	if !p.on {
		return s
	}
	return prefix + s + ansiReset
}

func (p *palette) bold(s string) string   { return p.wrap(ansiBold, s) }
func (p *palette) dim(s string) string    { return p.wrap(ansiDim, s) }
func (p *palette) accent(s string) string { return p.wrap(ansiAmber, s) }

// isTTY returns true when f refers to a character device (i.e. a terminal),
// not a pipe or file. Works on macOS and Linux without external deps.
func isTTY(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}
