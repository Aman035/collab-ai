// Package state writes a snapshot of the running session to
// ~/.collab-ai/state.json so that out-of-band readers (collab-ai status,
// collab-ai export) can see what's happening without their own MCP/Axl link.
//
// One-state-file model: a single running session per machine in v1. Multi
// session is not supported by status/export — flagged in docs/08-cli.md.
package state

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

// FileVersion lets readers refuse mismatched state shapes early.
const FileVersion = "0.1"

// File is the on-disk shape. Both writer and reader use this.
type File struct {
	FileVersion string              `json:"file_version"`
	Version     string              `json:"version"` // collab-ai protocol/build version
	SessionID   string              `json:"session_id"`
	StartedAt   time.Time           `json:"started_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	Role        string              `json:"role"`
	InviteCode  string              `json:"invite_code,omitempty"`
	PeerID      string              `json:"peer_id"`
	SharedDir   string              `json:"shared_dir"`
	Peers       []Peer              `json:"peers"`
	Entries     []protocol.LogEntry `json:"entries"`
	Files       []protocol.FileMeta `json:"files,omitempty"`
}

// Peer is what we show about a connected peer.
type Peer struct {
	ID       string    `json:"id"`
	JoinedAt time.Time `json:"joined_at"`
	Self     bool      `json:"self,omitempty"`
}

// Path returns the canonical state file location for this machine.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".collab-ai", "state.json"), nil
}

// Read parses the state file. Returns an os.IsNotExist error when no
// session is running.
func Read() (*File, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &f, nil
}

// Write atomically replaces the state file.
func Write(f *File) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(p), err)
	}
	tmp := p + ".tmp"
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Remove deletes the state file. Best-effort; swallows os.IsNotExist.
func Remove() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Writer drives periodic snapshots. The build closure is supplied by the
// CLI; it's called every tick to produce a fresh snapshot from live state
// (store entries, peer table, etc).
type Writer struct {
	build    func() *File
	interval time.Duration

	mu      sync.Mutex
	stopped bool
	done    chan struct{}
}

// NewWriter constructs a snapshot writer. Default interval is 1s.
func NewWriter(build func() *File, interval time.Duration) *Writer {
	if interval == 0 {
		interval = time.Second
	}
	return &Writer{build: build, interval: interval, done: make(chan struct{})}
}

// Run flushes a snapshot every interval until ctx is cancelled. On exit,
// removes the state file so out-of-band readers see "no active session".
func (w *Writer) Run(stopCh <-chan struct{}) {
	defer close(w.done)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	w.flush()
	for {
		select {
		case <-stopCh:
			if err := Remove(); err != nil {
				slog.Warn("state: remove on shutdown failed", "err", err)
			}
			return
		case <-t.C:
			w.flush()
		}
	}
}

func (w *Writer) flush() {
	f := w.build()
	if f == nil {
		return
	}
	f.UpdatedAt = time.Now().UTC()
	if f.FileVersion == "" {
		f.FileVersion = FileVersion
	}
	if err := Write(f); err != nil {
		slog.Warn("state: write failed", "err", err)
	}
}
