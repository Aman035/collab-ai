// Package sync keeps peers in sync. It subscribes to local Store changes,
// broadcasts them as WireMessages, and applies inbound messages to the local
// Store. Echo suppression compares envelope PeerID to our own — never
// Axl's lossy X-From-Peer-Id header.
//
// File changes flow through fsnotify on the shared dir; the Store owns CRDT
// merge (LWW-Register + add-wins) and the Sync Engine mirrors accepted
// decisions to disk.
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/Aman035/collab-ai/internal/shareddir"
	"github.com/Aman035/collab-ai/internal/store"
	"github.com/Aman035/collab-ai/internal/transport"
	"github.com/Aman035/collab-ai/pkg/protocol"
)

// recentWriteTTL is how long a peer-applied write suppresses fsnotify events
// for that path. Longer than typical fsnotify latency; short enough that
// genuine edits made shortly after aren't suppressed.
const recentWriteTTL = 1500 * time.Millisecond

// fsDebounce coalesces rapid fsnotify events for the same path. A typical
// editor save fires 3-5 events within ~50 ms.
const fsDebounce = 100 * time.Millisecond

// Engine connects the local Store to the Transport and (optionally) watches
// a shared directory for file changes.
type Engine struct {
	store     *store.Store
	transport transport.Transport
	peerID    string
	sharedDir string
	filter    *shareddir.Filter

	recentMu     sync.Mutex
	recentWrites map[string]time.Time
}

// New constructs an Engine. If sharedDir is empty, the fsnotify watcher does
// not run — useful in tests that only exercise the log path.
func New(s *store.Store, t transport.Transport, peerID, sharedDir string) *Engine {
	e := &Engine{
		store:        s,
		transport:    t,
		peerID:       peerID,
		sharedDir:    sharedDir,
		recentWrites: make(map[string]time.Time),
	}
	if sharedDir != "" {
		e.filter = shareddir.NewFilter(sharedDir)
	}
	return e
}

// Run blocks until ctx is cancelled. Starts the store-change subscription,
// the inbound transport receive loop, and (if sharedDir != "") the fsnotify
// watcher.
func (e *Engine) Run(ctx context.Context) error {
	if e.sharedDir != "" {
		go e.watchDir(ctx)
	}

	changes := e.store.Subscribe()
	inbound := e.transport.Receive()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch, ok := <-changes:
			if !ok {
				return nil
			}
			if err := e.handleLocalChange(ch); err != nil {
				slog.Warn("sync: outbound failed", "err", err)
			}
		case msg, ok := <-inbound:
			if !ok {
				return nil
			}
			if err := e.handleInboundMessage(msg); err != nil {
				slog.Warn("sync: inbound failed", "err", err, "kind", msg.Kind, "peer", msg.PeerID)
			}
		}
	}
}

// Replay sends every existing log entry and file from the local store to a
// single peer. Called by the host when a fresh joiner connects so they
// arrive with the full session context — without it, late joiners would
// only see messages and files created after their handshake.
//
// Log entries replay through SendTo with their original PeerID; the
// receiver's CRDT (G-Set on log, LWW-Register on files) handles dedup
// and ordering correctly. File contents are read from disk on demand.
func (e *Engine) Replay(peerID string) error {
	for _, entry := range e.store.EntriesSince(time.Time{}) {
		raw, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		_ = e.transport.SendTo(peerID, protocol.WireMessage{
			Kind:      protocol.KindLogEntry,
			Payload:   raw,
			PeerID:    entry.PeerID,
			Timestamp: entry.Timestamp,
		})
	}
	if e.sharedDir == "" {
		return nil
	}
	for _, meta := range e.store.ListFiles("") {
		abs := filepath.Join(e.sharedDir, meta.Path)
		content, err := os.ReadFile(abs)
		if err != nil {
			slog.Warn("sync: replay read", "path", meta.Path, "err", err)
			continue
		}
		if len(content) > shareddir.MaxFileBytes {
			continue
		}
		payload, err := json.Marshal(protocol.FileChunkPayload{
			Path:    meta.Path,
			Content: content,
			Hash:    meta.Hash,
		})
		if err != nil {
			continue
		}
		_ = e.transport.SendTo(peerID, protocol.WireMessage{
			Kind:      protocol.KindFileChunk,
			Payload:   payload,
			PeerID:    meta.PeerID,
			Timestamp: meta.ModTime,
		})
	}
	return nil
}

// handleLocalChange broadcasts log_entry messages for local-source store
// changes. File changes don't fan out via the store-change channel — the
// store carries metadata only; content travels in file_chunk messages
// emitted directly by watchDir.
func (e *Engine) handleLocalChange(ch store.Change) error {
	if ch.Source != store.SourceLocal {
		return nil
	}
	if ch.Kind != store.KindEntryAppended {
		return nil
	}
	if ch.Entry == nil {
		return errors.New("entry_appended change with nil Entry")
	}
	raw, err := json.Marshal(*ch.Entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	return e.transport.Send(protocol.WireMessage{
		Kind:      protocol.KindLogEntry,
		Payload:   raw,
		PeerID:    e.peerID,
		Timestamp: ch.Entry.Timestamp,
	})
}

// handleInboundMessage applies a peer-originated message to the local Store.
// Echo suppression: drop messages whose envelope PeerID matches our own.
func (e *Engine) handleInboundMessage(msg protocol.WireMessage) error {
	if msg.PeerID == e.peerID {
		return nil
	}
	switch msg.Kind {
	case protocol.KindLogEntry:
		var entry protocol.LogEntry
		if err := json.Unmarshal(msg.Payload, &entry); err != nil {
			return fmt.Errorf("unmarshal log_entry: %w", err)
		}
		return e.store.AppendEntry(entry, store.SourcePeer)

	case protocol.KindFileChunk:
		var p protocol.FileChunkPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			return fmt.Errorf("unmarshal file_chunk: %w", err)
		}
		return e.applyInboundFileChunk(msg, p)

	case protocol.KindHello, protocol.KindHelloAck, protocol.KindGoodbye:
		slog.Debug("sync: control message reached engine", "kind", msg.Kind)
		return nil

	default:
		slog.Warn("sync: unknown message kind", "kind", msg.Kind)
		return nil
	}
}

// applyInboundFileChunk validates a peer file_chunk, asks the Store to merge,
// and on accepted writes mirrors the change to disk.
func (e *Engine) applyInboundFileChunk(msg protocol.WireMessage, p protocol.FileChunkPayload) error {
	if e.sharedDir == "" {
		return nil
	}
	if err := shareddir.ValidatePath(p.Path); err != nil {
		return fmt.Errorf("inbound file path invalid: %w", err)
	}
	if e.filter != nil && e.filter.ShouldSkip(p.Path) {
		return nil
	}

	meta := protocol.FileMeta{
		Path:    p.Path,
		PeerID:  msg.PeerID,
		ModTime: msg.Timestamp,
		Hash:    p.Hash,
	}
	abs := filepath.Join(e.sharedDir, p.Path)

	if p.Deleted {
		accepted, err := e.store.DeleteFile(p.Path, meta, store.SourcePeer)
		if err != nil {
			return fmt.Errorf("store.DeleteFile: %w", err)
		}
		if !accepted {
			return nil
		}
		e.markRecentWrite(p.Path)
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			slog.Warn("sync: remove failed", "path", p.Path, "err", err)
		}
		return nil
	}

	if len(p.Content) > shareddir.MaxFileBytes {
		return fmt.Errorf("inbound file_chunk too large: %d bytes", len(p.Content))
	}
	if hashHex(p.Content) != p.Hash {
		return errors.New("inbound file_chunk hash mismatch")
	}

	meta.Size = int64(len(p.Content))
	accepted, err := e.store.UpsertFile(meta, store.SourcePeer)
	if err != nil {
		return fmt.Errorf("store.UpsertFile: %w", err)
	}
	if !accepted {
		return nil
	}
	e.markRecentWrite(p.Path)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	if err := os.WriteFile(abs, p.Content, 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// watchDir runs the fsnotify loop. Coalesces rapid events per path with a
// short debounce, then either reads + broadcasts or sends a delete.
func (e *Engine) watchDir(ctx context.Context) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("sync: fsnotify watcher", "err", err)
		return
	}
	defer w.Close()

	if err := addRecursive(w, e.sharedDir); err != nil {
		slog.Error("sync: addRecursive", "err", err)
		return
	}

	pending := make(map[string]*time.Timer)

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			rel, err := filepath.Rel(e.sharedDir, ev.Name)
			if err != nil || rel == "." || rel == "" {
				continue
			}
			rel = filepath.ToSlash(rel)
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addRecursive(w, ev.Name)
					continue
				}
			}
			if e.filter != nil && e.filter.ShouldSkip(rel) {
				continue
			}
			if t, exists := pending[rel]; exists {
				t.Stop()
			}
			path := rel
			pending[path] = time.AfterFunc(fsDebounce, func() {
				e.processFsEvent(path)
			})
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("sync: fsnotify error", "err", err)
		}
	}
}

// processFsEvent runs after debounce. Reads the file (or notes it's gone),
// asks the store to merge, and broadcasts on accepted writes.
func (e *Engine) processFsEvent(rel string) {
	if e.wasRecentWrite(rel) {
		return
	}
	abs := filepath.Join(e.sharedDir, rel)
	info, err := os.Stat(abs)
	if errors.Is(err, os.ErrNotExist) {
		e.handleLocalDelete(rel)
		return
	}
	if err != nil {
		slog.Warn("sync: stat", "path", rel, "err", err)
		return
	}
	if info.IsDir() {
		return
	}
	if info.Size() > shareddir.MaxFileBytes {
		slog.Warn("sync: file too large, not synced", "path", rel, "size", info.Size())
		return
	}
	content, err := readWithRetry(abs)
	if err != nil {
		slog.Warn("sync: read", "path", rel, "err", err)
		return
	}
	now := time.Now().UTC()
	hash := hashHex(content)
	meta := protocol.FileMeta{
		Path:    rel,
		Size:    int64(len(content)),
		ModTime: now,
		Hash:    hash,
		PeerID:  e.peerID,
	}
	accepted, err := e.store.UpsertFile(meta, store.SourceLocal)
	if err != nil || !accepted {
		return
	}
	payload, _ := json.Marshal(protocol.FileChunkPayload{
		Path:    rel,
		Content: content,
		Hash:    hash,
		Deleted: false,
	})
	_ = e.transport.Send(protocol.WireMessage{
		Kind:      protocol.KindFileChunk,
		Payload:   payload,
		PeerID:    e.peerID,
		Timestamp: now,
	})
}

func (e *Engine) handleLocalDelete(rel string) {
	now := time.Now().UTC()
	meta := protocol.FileMeta{Path: rel, ModTime: now, PeerID: e.peerID}
	accepted, err := e.store.DeleteFile(rel, meta, store.SourceLocal)
	if err != nil || !accepted {
		return
	}
	payload, _ := json.Marshal(protocol.FileChunkPayload{Path: rel, Deleted: true})
	_ = e.transport.Send(protocol.WireMessage{
		Kind:      protocol.KindFileChunk,
		Payload:   payload,
		PeerID:    e.peerID,
		Timestamp: now,
	})
}

// addRecursive walks dir and adds every subdirectory to the watcher.
func addRecursive(w *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return w.Add(p)
		}
		return nil
	})
}

// readWithRetry copes with editors that briefly hold a file open after save.
func readWithRetry(path string) ([]byte, error) {
	var lastErr error
	for i := range 3 {
		if i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
		b, err := os.ReadFile(path)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func hashHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (e *Engine) markRecentWrite(path string) {
	e.recentMu.Lock()
	defer e.recentMu.Unlock()
	e.recentWrites[path] = time.Now()
	cutoff := time.Now().Add(-recentWriteTTL)
	for k, v := range e.recentWrites {
		if v.Before(cutoff) {
			delete(e.recentWrites, k)
		}
	}
}

func (e *Engine) wasRecentWrite(path string) bool {
	e.recentMu.Lock()
	defer e.recentMu.Unlock()
	if ts, ok := e.recentWrites[path]; ok && time.Since(ts) < recentWriteTTL {
		delete(e.recentWrites, path)
		return true
	}
	delete(e.recentWrites, path)
	return false
}
