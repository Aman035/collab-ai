package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Aman035/collab-ai/internal/handle"
)

// sessionRoot is the parent dir under which collab-ai manages per-session
// folders. ~/collab-ai/<session-id>/ holds the shared dir and (later) any
// per-session config or state.
func sessionRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, "collab-ai"), nil
}

// allocateSessionDir returns (sessionID, sharedDir) for a fresh host session.
// Picks a fun-name session ID that doesn't already collide with an existing
// directory under ~/collab-ai/.
func allocateSessionDir() (sessionID, sharedDir string, err error) {
	root, err := sessionRoot()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", root, err)
	}
	sessionID = handle.NewUnique(func(name string) bool {
		_, err := os.Stat(filepath.Join(root, name))
		return err == nil
	})
	sharedDir = filepath.Join(root, sessionID, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", sharedDir, err)
	}
	return sessionID, sharedDir, nil
}

// resolveDir returns the user-supplied --dir if non-empty, otherwise the
// fresh managed dir under ~/collab-ai/. The returned sessionID is empty
// when --dir was overridden (we don't claim a name we didn't create).
func resolveDir(userDir string) (sessionID, sharedDir string, err error) {
	if userDir != "" {
		return "", userDir, nil
	}
	return allocateSessionDir()
}
