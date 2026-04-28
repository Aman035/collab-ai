# 05 · Component: Sync Engine

**Package:** `internal/sync`
**Build estimate:** ~500 LOC (largest component)
**Depends on:** `internal/store`, `internal/transport`, `internal/shareddir`, `pkg/protocol`

## Purpose

Keep peers in sync. Watches the local store and the local file system; broadcasts outbound changes to peers; applies inbound changes from peers.

This is where the protocol's edge cases live. Most of the bugs you'll write will be here. Read this whole doc before starting.

## Responsibilities

1. Subscribe to Store changes; serialize local-origin changes into wire messages; push to Transport.
2. Watch `./shared/` via fsnotify; on file change, hash + write FileMeta to Store; broadcast a `file_chunk` message with content.
3. Receive wire messages from Transport; deserialize; apply to Store and (for files) write content to `./shared/`.
4. **CRDT merge**: trust the Store. Hand inbound metadata to `Store.UpsertFile` / `Store.DeleteFile` and let the per-file LWW-Register + add-wins tombstones decide. Mirror the Store's decision to disk: write the file iff the upsert was accepted; remove the file iff the delete was accepted.
5. **Echo suppression**: drop incoming messages whose `peer_id` matches our own.

## Non-goals

- No op-based or character-level text CRDTs. v1 uses simple state-based CRDTs only — log G-Set, per-file LWW-Register, add-wins file set — which the Store implements. The Sync Engine is a transport for those state updates, not a merge algorithm of its own.
- No partial-file diffs. v1 sends whole files (cap ~256 KB; refuse larger files with a warning).
- No symlink or binary smarts. Plain files only. Skip noisy directories per gitignore-style rules.

## Public interface

```go
package sync

import (
  "context"
  "github.com/Aman035/colabAI/internal/store"
  "github.com/Aman035/colabAI/internal/transport"
)

type Engine struct {
  store     store.Store
  transport transport.Transport
  sharedDir string
  peerID    string
  // ...internal state
}

func New(s store.Store, t transport.Transport, sharedDir, peerID string) *Engine

// Run blocks until ctx is cancelled. Starts:
//   - the Store change subscription (outbound log/file changes → Transport)
//   - the fsnotify watcher on sharedDir (outbound file changes → Store + Transport)
//   - the Transport receive loop (inbound WireMessages → Store + sharedDir)
func (e *Engine) Run(ctx context.Context) error
```

## Behavior

### Outbound: Store change → Transport

```
for change := range store.Subscribe():
    if change.Source == "peer":
        continue                              // it came from a peer; don't loop it back
    msg := serializeChange(change, e.peerID)  // produces a WireMessage
    transport.Send(msg)                       // best-effort broadcast
```

### Outbound: file change → Store

```
fsnotify event on ./shared/X:
    if shouldSkip(X):
        continue                              // gitignore, .git, node_modules, etc.
    if size(X) > 256 KB:
        log.Warn("file too large, not synced", X)
        continue
    content := readFile(X)
    hash := sha256(content)
    meta := FileMeta{Path: X, Size: ..., ModTime: now, Hash: hash, PeerID: e.peerID}
    store.UpsertFile(meta, "local")           // triggers outbound via the loop above
    // ALSO send the content as a file_chunk message
    transport.Send(WireMessage{
      Kind: "file_chunk",
      Payload: FileChunkPayload{Path: X, Content: content, Hash: hash, Deleted: false},
      PeerID: e.peerID,
      Timestamp: now,
    })
```

Note that we send `file_chunk` directly here rather than relying on the Store-change loop — the Store doesn't carry file contents, only metadata. The two paths are deliberate:

- Store changes broadcast **metadata** (used for log entries and for "file deleted" awareness).
- Direct `file_chunk` sends carry **content**.

### Inbound: WireMessage → Store + filesystem

```
for msg := range transport.Receive():
    if msg.PeerID == e.peerID:
        continue                              // echo; ignore
    switch msg.Kind:
    case "log_entry":
        store.AppendEntry(msg.Payload.(LogEntry), "peer")
    case "file_chunk":
        // The Store owns CRDT merge. We just hand it the metadata and mirror
        // its decision to disk. Use the envelope's PeerID as the register's
        // peer-ID component — that's what makes the tiebreak deterministic.
        meta := FileMeta{
          Path:    msg.Payload.Path,
          Size:    int64(len(msg.Payload.Content)),
          ModTime: msg.Timestamp,
          Hash:    msg.Payload.Hash,
          PeerID:  msg.PeerID,
        }
        if msg.Payload.Deleted:
            accepted := store.DeleteFile(msg.Payload.Path, meta, "peer")
            if accepted:
                os.Remove(filepath.Join(sharedDir, msg.Payload.Path))
        else:
            accepted := store.UpsertFile(meta, "peer")
            if accepted:
                os.WriteFile(filepath.Join(sharedDir, msg.Payload.Path), msg.Payload.Content, 0644)
    case "hello", "goodbye":
        // log peer events; don't touch store/files
```

### Echo suppression — the most important rule

**Every WireMessage carries a `PeerID` field** identifying the peer that originated it. Our own messages will be received back from the network in some Axl topologies (depends on the transport's broadcast semantics). The Sync Engine **must** drop messages where `PeerID == e.peerID`.

This is a one-line check, but forgetting it produces a tight feedback loop where every local change gets re-applied by the same peer that emitted it, which causes timestamps to thrash and files to ping-pong.

## File path filtering

Skip these paths in the file watcher:

- Anything starting with `.git/`
- Anything starting with `.venv/`, `venv/`, `__pycache__/`
- Anything starting with `node_modules/`
- Anything starting with `dist/`, `build/`, `target/`
- Anything matching `*.lock`
- Anything in `.gitignore` (best-effort — read it once at startup; don't watch it)

Implement this in `internal/shareddir`; the Sync Engine just calls `shareddir.ShouldSkip(path)`.

## Failure modes

| Situation | Behavior |
|-----------|----------|
| Echo loop (own message back) | Drop based on `PeerID` match. **One-line check; do not skip.** |
| File written while being read | Read with up to 3 retries (10ms backoff). Skip on persistent failure with warning log. |
| Inbound file too large | Reject; log warning; continue. (Should never happen if all peers respect 256 KB cap.) |
| Disk write failure on inbound file | Log; do not crash. User can retry by editing on the source side. |
| fsnotify queue overrun | Log; do a full re-scan of ./shared/ to recover. |
| Transport.Send fails (peer disconnected) | Log; continue. Sync Engine doesn't retry; transient. |
| Two peers edit the same file simultaneously | LWW-Register merge in the Store decides — greater `(ModTime, PeerID)` wins. Document this in the README. |
| Concurrent edit + delete of the same file | Add-wins. The edit survives if its `(ModTime, PeerID)` exceeds the delete's; otherwise the delete sticks (tombstone). Store-resident logic, not Sync Engine logic. |

## Implementation notes

- Use `github.com/fsnotify/fsnotify`.
- Coalesce rapid fsnotify events for the same path. A single editor save can fire 3-5 events; debounce to one event per path per ~100ms.
- The Engine's `Run` method should return only when `ctx` is cancelled. Don't bury internal goroutines without a way to stop them.
- All paths exchanged on the wire are **relative to ./shared/**, never absolute. Filter for `..` traversal on inbound messages — refuse paths containing `..`.

## Acceptance criteria

A two-process integration test (or two goroutines pretending to be peers, sharing an in-memory transport stub) where:

1. Peer A appends a log entry → it appears in peer B's Store within a tick.
2. Peer A creates a file in `./shared/test.txt` → peer B's filesystem has the same file.
3. Peer A and peer B both edit the same file concurrently → both converge on the same content (greater `(ModTime, PeerID)` wins; the test should pin a deterministic winner by fixing peer IDs).
4. Peer A deletes a file while peer B concurrently edits it → both peers converge on B's edit (add-wins). A delivered out-of-order (delete arrives at B after the edit, edit arrives at A after the delete) still converges.
5. An echoed message (PeerID == self) is dropped without effect.
6. A 1MB file edit is logged but not synced.
