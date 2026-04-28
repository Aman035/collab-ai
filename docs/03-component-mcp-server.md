# 03 · Component: MCP Server

**Package:** `internal/mcp`
**Build estimate:** ~200 LOC
**Depends on:** `internal/store` (via Store interface)

## Purpose

Bridge between the local AI coding agent and the rest of collab-ai. The agent sees shared state through this layer's tools.

## Responsibilities

- Expose three tools to the agent over stdio MCP transport.
- Translate tool calls into Session State Store reads/writes.
- Return structured responses the agent can reason about.

## Non-goals

- Does **not** enforce permissions. Anything in the local store is exposed.
- Does **not** call the agent's API. The agent owns its own model calls.
- Does **not** handle file content directly. File ops go through `list_shared_files`; the agent edits files via its own filesystem tools.

## Public interface

### Tool: `get_shared_log`

Returns the full conversation history from the local Store.

```
input:  { since_timestamp?: string }     // RFC3339; if set, only return entries after this
output: {
  entries: LogEntry[],
  current_peers: string[]                // peer IDs currently connected
}
```

`LogEntry` shape (defined in `pkg/protocol`):

```go
type LogEntry struct {
  ID        string    `json:"id"`         // ULID
  Timestamp time.Time `json:"timestamp"`
  PeerID    string    `json:"peer_id"`
  Role      string    `json:"role"`       // "user" | "assistant"
  Content   string    `json:"content"`
}
```

### Tool: `post_to_log`

Appends a new entry to the local Store. The Store emits a Change; the Sync Engine picks it up and broadcasts it.

```
input:  { role: "user" | "assistant", content: string }
output: { entry_id: string, timestamp: string }
```

The MCP server fills in `id` (new ULID), `timestamp` (now), and `peer_id` (this peer's ID) before calling the Store. The agent does not provide them.

### Tool: `list_shared_files`

Returns metadata for files in `./shared/`.

```
input:  { path?: string }                 // optional prefix filter, relative to ./shared/
output: { files: FileMeta[] }
```

`FileMeta` shape:

```go
type FileMeta struct {
  Path    string    `json:"path"`         // relative to ./shared/
  Size    int64     `json:"size"`
  ModTime time.Time `json:"modified"`
  Hash    string    `json:"hash"`         // sha256 hex
  PeerID  string    `json:"peer_id"`      // originating peer; LWW-Register tiebreak component
}
```

`(ModTime, PeerID)` is the LWW-Register version. Two peers writing at the same wall-clock time will deterministically converge because PeerID breaks the tie.

## Failure modes

| Situation | Behavior |
|-----------|----------|
| Agent disconnects (stdio close) | Server exits cleanly. Other components stay running. |
| Malformed tool input | Return MCP error response with descriptive message. |
| Large log responses | Paginate via `since_timestamp`. Cap individual responses at 500 entries. |
| Store returns error | Wrap and return as MCP error. |

## Implementation notes

- Use `github.com/mark3labs/mcp-go` (verify it's still the right choice on day one).
- Stdio is the only transport. Don't add SSE or WebSocket variants in v1.
- The MCP server is started by the CLI when `collab-ai host` or `collab-ai join` runs and stays alive for the duration of the session.
- All three tools are thin wrappers around Store methods. There should be near-zero business logic in this package.

## Acceptance criteria

A Go test that:

1. Starts a Store with a few seeded log entries.
2. Starts the MCP server pointed at that Store, on an in-memory pipe.
3. Sends a `get_shared_log` MCP request and receives the seeded entries.
4. Sends a `post_to_log` request and verifies the entry lands in the Store.
5. Sends a `list_shared_files` request against a temp directory and verifies the file metadata.
