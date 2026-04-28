# 04 · Component: Session State Store

**Package:** `internal/store`
**Build estimate:** ~300 LOC
**Depends on:** `pkg/protocol` (for LogEntry, FileMeta types)

## Purpose

The single source of truth for what's in the session locally. Append-only conversation log + index of shared files. Every other component reads from or writes to it.

## Responsibilities

- Maintain an in-memory append-only log of conversation entries.
- Maintain an in-memory file index keyed by relative path.
- Provide thread-safe read/write access (concurrent MCP and Sync Engine writes).
- Emit change events that the Sync Engine subscribes to.

## Non-goals

- Does **not** persist to disk in v1. Sessions are ephemeral.
- Does **not** resolve conflicts beyond the CRDT primitives. The log is a G-Set (entries dedupe by `id`); each file is an LWW-Register keyed on `(timestamp, peer_id)`; the file set is add-wins (a concurrent edit of a deleted file revives it).
- Does **not** store file contents. Only metadata. File contents live in `./shared/`.

## Public interface

```go
package store

import (
  "time"
  "github.com/Aman035/collab-ai/pkg/protocol"
)

type Change struct {
  Kind    string                  // "entry_appended" | "file_upserted" | "file_deleted"
  Entry   *protocol.LogEntry      // populated when Kind == "entry_appended"
  File    *protocol.FileMeta      // populated for file changes
  Source  string                  // "local" | "peer" — local origin should be broadcast; peer origin should not
}

type Store interface {
  // Log operations
  AppendEntry(entry protocol.LogEntry, source string) error
  EntriesSince(t time.Time) []protocol.LogEntry

  // File index operations.
  // UpsertFile and DeleteFile return (accepted, err): accepted == true when the
  // CRDT merge took effect (so the caller should mirror to disk); accepted ==
  // false when the op lost to a newer (ModTime, PeerID) — not an error, just
  // stale. err is non-nil only on internal failures (locking, etc).
  UpsertFile(meta protocol.FileMeta, source string) (accepted bool, err error)
  DeleteFile(path string, meta protocol.FileMeta, source string) (accepted bool, err error)
  ListFiles(prefix string) []protocol.FileMeta
  GetFile(path string) (protocol.FileMeta, bool)

  // Change subscription — Sync Engine calls this once at startup
  Subscribe() <-chan Change

  // Lifecycle
  Close() error
}
```

`source` distinguishes locally-originated changes (which should be broadcast to peers) from peer-originated changes (which should not — they came from a peer, broadcasting them again would create an echo loop).

## Data structures

```go
type tombstone struct {
  ModTime time.Time
  PeerID  string
  At      time.Time   // when the tombstone was recorded; for TTL eviction
}

type storeImpl struct {
  mu         sync.RWMutex
  entries    []protocol.LogEntry           // G-Set, sorted by timestamp
  entryIDs   map[string]struct{}           // for dedup on append
  files      map[string]protocol.FileMeta  // path → LWW-Register state
  tombstones map[string]tombstone          // path → last delete's (ModTime, PeerID) for add-wins resolution
  subs       []chan Change                 // change subscribers
}
```

## Behavior

### `AppendEntry`

1. If the entry's ID already exists in `entryIDs`, return nil (idempotent).
2. Acquire write lock.
3. Find insertion position by timestamp (binary search; entries are kept sorted).
4. Insert. Record ID.
5. Emit `Change{Kind: "entry_appended", Entry: &entry, Source: source}` to all subscribers.

### `UpsertFile`

LWW-Register merge per file, with deterministic tiebreak.

1. Acquire write lock.
2. Compare `(incoming.ModTime, incoming.PeerID)` with the existing entry's `(ModTime, PeerID)` lexicographically. If the existing pair is greater-or-equal, return nil — incoming is stale.
3. Otherwise overwrite.
4. Emit `Change{Kind: "file_upserted", File: &meta, Source: source}`.

### `DeleteFile`

Add-wins: a delete only takes effect if no concurrent or later upsert exists.

1. Acquire write lock.
2. If an existing entry has `(ModTime, PeerID) > (incoming.ModTime, incoming.PeerID)`, drop the delete (an edit beat it). Return nil.
3. Otherwise remove and emit `Change{Kind: "file_deleted", File: &<previous>, Source: source}`. The deletion's `(ModTime, PeerID)` is recorded in a small **tombstone map** so a later upsert with an older `(ModTime, PeerID)` can still be rejected as stale; the tombstone may be evicted after a generous TTL (e.g. 1 hour) since v1 sessions are short.

### `Subscribe`

Returns a buffered channel (capacity ~256). If a subscriber is slow and the buffer fills, drop the oldest events with a warning log — do not block.

## Failure modes

| Situation | Behavior |
|-----------|----------|
| Duplicate entry ID | Return nil silently (G-Set idempotence). |
| Out-of-order entry from peer | Insert in correct timestamp position. Consumers always sort by timestamp. |
| Concurrent file write + delete | Add-wins: whichever has the greater `(ModTime, PeerID)` survives. Tombstones make this deterministic regardless of arrival order. |
| Stale upsert arriving after a delete | Rejected via the tombstone map. |
| Slow subscriber | Drop oldest events on a full channel; log a warning. Do not block writers. |
| Memory pressure on long sessions | Not handled in v1. Sessions are short-lived by design. Tombstones evict after ~1 hour. |

## Implementation notes

- ULIDs make timestamp ordering naturally consistent across peers (the timestamp is part of the ID). When two entries have the same wall-clock time, ULID byte ordering decides who wins. Don't fight this — embrace it.
- Use `sync.RWMutex` not `sync.Mutex`. `EntriesSince` and `ListFiles` are read-heavy.
- The `entries` slice is small (target: <10K entries per session). Linear scans are fine for `EntriesSince` if binary search feels premature.

## Acceptance criteria

Tests that demonstrate:

1. Concurrent `AppendEntry` calls from multiple goroutines yield a sorted, deduplicated log (G-Set semantics).
2. `EntriesSince` returns only entries strictly after the given timestamp.
3. `UpsertFile` with an older `(ModTime, PeerID)` is silently dropped (LWW-Register semantics, with peer-ID tiebreak verified on identical timestamps).
4. A `DeleteFile` followed by a stale `UpsertFile` (older `(ModTime, PeerID)`) leaves the file deleted (tombstone honored). A `DeleteFile` followed by a fresh `UpsertFile` revives the file (add-wins).
5. A subscriber receives a `Change` for every successful write.
6. A blocked subscriber doesn't block writers — the warning shows up in logs and writes proceed.
