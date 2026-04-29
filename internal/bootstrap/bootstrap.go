// Package bootstrap brings up the dependencies collab-ai needs at runtime.
// Right now that's exactly one thing: the Gensyn Axl `node` binary, which
// we resolve into ~/.collab/bin/axl-node and build from source on first use.
//
// The goal is "one command to install collab, no second binary to chase":
// users get `collab` on their PATH, run `collab create`, and we transparently
// fetch + build Axl into a collab-managed directory.
package bootstrap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AxlRepoURL is where we clone Gensyn Axl from on first-time bootstrap.
const AxlRepoURL = "https://github.com/gensyn-ai/axl.git"

// HomeDir returns the ~/.collab directory, creating it if needed.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".collab")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return dir, nil
}

// AxlNodePath is the canonical location of the Axl daemon binary inside the
// collab-managed dir (~/.collab/bin/axl-node).
func AxlNodePath() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "bin", "axl-node"), nil
}

// EnsureAxl resolves the axl-node binary, building it on first call:
//
//  1. If $COLLAB_AXL_NODE is set and points at an executable, use it.
//  2. Otherwise, if ~/.collab/bin/axl-node exists, use it.
//  3. Otherwise, clone gensyn-ai/axl, `go build` it into ~/.collab/bin/, and
//     use that.
//
// Returns the absolute path to the binary, or an error whose message tells
// the user the next concrete step to fix it.
func EnsureAxl() (string, error) {
	if v := os.Getenv("COLLAB_AXL_NODE"); v != "" {
		if isExecutable(v) {
			return v, nil
		}
		fmt.Fprintf(os.Stderr, "warn: COLLAB_AXL_NODE=%q is not executable; falling back to managed install\n", v)
	}

	target, err := AxlNodePath()
	if err != nil {
		return "", err
	}
	if isExecutable(target) {
		return target, nil
	}
	if err := buildAxl(target); err != nil {
		return "", err
	}
	return target, nil
}

// buildAxl clones the upstream repo into a temp dir and builds the daemon
// into target. Surfaces clone + build output to the user via stderr so a
// slow first run doesn't feel hung.
func buildAxl(target string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("axl-node not installed and 'git' is not on PATH — install git, or set COLLAB_AXL_NODE to an existing axl-node binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("axl-node not installed and 'go' is not on PATH — install Go 1.22+ from https://go.dev, or set COLLAB_AXL_NODE")
	}

	tmp, err := os.MkdirTemp("", "collab-axl-build-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	fmt.Fprintln(os.Stderr, "→ first-time setup: building Gensyn Axl into ~/.collab/bin/ (one-time, ~30s)")

	clone := exec.Command("git", "clone", "--depth=1", AxlRepoURL, tmp)
	clone.Stdout = os.Stderr
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		return fmt.Errorf("clone axl: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}

	build := exec.Command("go", "build", "-o", target, "./cmd/node/")
	build.Dir = tmp
	// GOTOOLCHAIN=auto lets Go fetch a newer toolchain if go.mod requires
	// one beyond what the user has installed (Axl currently wants 1.25+).
	build.Env = append(os.Environ(), "GOTOOLCHAIN=auto")
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("build axl: %w", err)
	}

	fmt.Fprintln(os.Stderr, "✓ axl-node ready at "+target)
	return nil
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Mode()&0o111 != 0
}
