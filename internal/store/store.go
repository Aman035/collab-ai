// Package store is the local source of truth for a session: an append-only
// conversation log (G-Set keyed by ULID) plus, in M2, an LWW-Register file
// index. M1 implements the log only.
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
)

// Change is a single store mutation.
type Change struct {
	Kind   string
	Entry  *protocol.LogEntry
	Source string
}

// Store holds the conversation log in memory. Safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	entries  []protocol.LogEntry
	entryIDs map[string]struct{}
	changes  chan Change
	closed   bool
}

// New returns an empty Store with a buffered Change channel.
func New() *Store {
	return &Store{
		entryIDs: make(map[string]struct{}),
		changes:  make(chan Change, 256),
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
