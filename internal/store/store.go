// Package store is the local source of truth for a session: an append-only
// conversation log (G-Set keyed by ULID) plus an LWW-Register file index
// with add-wins delete semantics.
package store

import (
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

// Source distinguishes locally-originated changes (broadcast) from
// peer-originated changes (do not broadcast — would create an echo loop).
const (
	SourceLocal = "local"
	SourcePeer  = "peer"
)

// Change kinds emitted on Subscribe().
const (
	KindEntryAppended = "entry_appended"
	KindFileUpserted  = "file_upserted"
	KindFileDeleted   = "file_deleted"
)

// Change is a single store mutation. Entry is set for log changes; File is
// set for file changes.
type Change struct {
	Kind   string
	Entry  *protocol.LogEntry
	File   *protocol.FileMeta
	Source string
}

// tombstone records the (ModTime, PeerID) of the most recent delete for a
// path so a later stale upsert is rejected (add-wins).
type tombstone struct {
	ModTime time.Time
	PeerID  string
}

// Store holds the conversation log + file index in memory. Safe for
// concurrent use.
type Store struct {
	mu         sync.RWMutex
	entries    []protocol.LogEntry
	entryIDs   map[string]struct{}
	files      map[string]protocol.FileMeta
	tombstones map[string]tombstone
	changes    chan Change
	closed     bool
}

// New returns an empty Store with a buffered Change channel.
func New() *Store {
	return &Store{
		entryIDs:   make(map[string]struct{}),
		files:      make(map[string]protocol.FileMeta),
		tombstones: make(map[string]tombstone),
		changes:    make(chan Change, 256),
	}
}

// AppendEntry inserts e in timestamp order. Idempotent on ID. The change is
// emitted to subscribers with the given source ("local" or "peer").
func (s *Store) AppendEntry(e protocol.LogEntry, source string) error {
	if e.ID == "" {
		return errors.New("store: entry ID is required")
	}
	s.mu.Lock()
	if _, dup := s.entryIDs[e.ID]; dup {
		s.mu.Unlock()
		return nil
	}
	pos := sort.Search(len(s.entries), func(i int) bool {
		return s.entries[i].Timestamp.After(e.Timestamp)
	})
	s.entries = append(s.entries, protocol.LogEntry{})
	copy(s.entries[pos+1:], s.entries[pos:])
	s.entries[pos] = e
	s.entryIDs[e.ID] = struct{}{}
	s.mu.Unlock()

	s.emit(Change{Kind: KindEntryAppended, Entry: &e, Source: source})
	return nil
}

// EntriesSince returns entries strictly after t, in timestamp order.
func (s *Store) EntriesSince(t time.Time) []protocol.LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]protocol.LogEntry, 0, len(s.entries))
	for _, e := range s.entries {
		if e.Timestamp.After(t) {
			out = append(out, e)
		}
	}
	return out
}

// UpsertFile applies an LWW-Register merge for one file. accepted == true
// when the merge took effect; the caller (Sync Engine) should mirror to
// disk only on accepted writes.
//
// The register's version is the lexicographic pair (ModTime, PeerID). On
// equal ModTime, the larger PeerID wins, giving deterministic convergence.
func (s *Store) UpsertFile(meta protocol.FileMeta, source string) (bool, error) {
	if meta.Path == "" {
		return false, errors.New("store: file path is required")
	}
	s.mu.Lock()
	if t, ok := s.tombstones[meta.Path]; ok {
		if !laterThan(meta.ModTime, meta.PeerID, t.ModTime, t.PeerID) {
			s.mu.Unlock()
			return false, nil
		}
	}
	if existing, ok := s.files[meta.Path]; ok {
		if !laterThan(meta.ModTime, meta.PeerID, existing.ModTime, existing.PeerID) {
			s.mu.Unlock()
			return false, nil
		}
	}
	s.files[meta.Path] = meta
	delete(s.tombstones, meta.Path)
	s.mu.Unlock()

	out := meta
	s.emit(Change{Kind: KindFileUpserted, File: &out, Source: source})
	return true, nil
}

// DeleteFile applies add-wins delete semantics. accepted == true when the
// delete took effect; the caller should remove the file from disk only on
// accepted deletes. A concurrent edit with a newer (ModTime, PeerID) beats
// the delete.
func (s *Store) DeleteFile(path string, meta protocol.FileMeta, source string) (bool, error) {
	if path == "" {
		return false, errors.New("store: file path is required")
	}
	s.mu.Lock()
	var previous protocol.FileMeta
	if existing, ok := s.files[path]; ok {
		if laterThan(existing.ModTime, existing.PeerID, meta.ModTime, meta.PeerID) {
			s.mu.Unlock()
			return false, nil
		}
		previous = existing
		delete(s.files, path)
		s.tombstones[path] = tombstone{ModTime: meta.ModTime, PeerID: meta.PeerID}
		s.mu.Unlock()
		s.emit(Change{Kind: KindFileDeleted, File: &previous, Source: source})
		return true, nil
	}
	if t, ok := s.tombstones[path]; ok && laterThan(t.ModTime, t.PeerID, meta.ModTime, meta.PeerID) {
		s.mu.Unlock()
		return false, nil
	}
	s.tombstones[path] = tombstone{ModTime: meta.ModTime, PeerID: meta.PeerID}
	s.mu.Unlock()

	out := protocol.FileMeta{Path: path, PeerID: meta.PeerID, ModTime: meta.ModTime}
	s.emit(Change{Kind: KindFileDeleted, File: &out, Source: source})
	return true, nil
}

// ListFiles returns metadata for files whose paths begin with prefix.
// Empty prefix returns all files. Order is unspecified.
func (s *Store) ListFiles(prefix string) []protocol.FileMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]protocol.FileMeta, 0, len(s.files))
	for p, m := range s.files {
		if prefix == "" || hasPrefix(p, prefix) {
			out = append(out, m)
		}
	}
	return out
}

// GetFile returns metadata for one path, if known.
func (s *Store) GetFile(path string) (protocol.FileMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.files[path]
	return m, ok
}

// laterThan returns true iff (aTime, aPeer) > (bTime, bPeer) lexicographically.
func laterThan(aTime time.Time, aPeer string, bTime time.Time, bPeer string) bool {
	if aTime.After(bTime) {
		return true
	}
	if bTime.After(aTime) {
		return false
	}
	return aPeer > bPeer
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// Subscribe returns the change channel. M1: single subscriber (the Sync
// Engine). Multi-subscriber support deferred until needed.
func (s *Store) Subscribe() <-chan Change {
	return s.changes
}

// Close shuts the change channel. Idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.changes)
	return nil
}

// emit pushes c to subscribers without blocking. If the buffer is full, drop
// the oldest event and warn.
func (s *Store) emit(c Change) {
	select {
	case s.changes <- c:
		return
	default:
	}
	select {
	case dropped := <-s.changes:
		slog.Warn("store: subscriber slow, dropping oldest change", "kind", dropped.Kind)
	default:
	}
	select {
	case s.changes <- c:
	default:
		slog.Warn("store: subscriber blocked, dropping change", "kind", c.Kind)
	}
}
