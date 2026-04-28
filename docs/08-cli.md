# 08 · CLI Surface

User-facing commands. Implemented in `cmd/collab-ai/` using `github.com/spf13/cobra`.

## Commands

### `collab-ai host`

Starts a session as the host.

```
$ collab-ai host [flags]

Flags:
  --dir string       Directory to share (default "./shared")
  --port int         Local port for Axl listener (default 0 = auto)
  --quiet            Suppress non-essential output
```

**Behavior:**

1. Validate `--dir` exists; create if it doesn't.
2. Start the Store, Sync Engine, and Transport.
3. Call `transport.Host()` to get the invite.
4. Print the invite code (large, prominent — this is the thing the user shares).
5. Start the MCP server on stdio.

  Wait — this is a complication: the MCP server takes over stdio, but the user wants to see status output too. Resolve by:
  - Print invite + setup instructions to **stderr** before the MCP server starts.
  - The MCP server uses **stdout** exclusively for MCP framing.
  - Status updates (peer joined, peer left) go to stderr.

6. Block until SIGINT or the agent disconnects.

**Sample output (to stderr):**

```
collab-ai v0.1.0 — hosting session

  Invite code:  COLLAB-7K2P-9XLM
  Shared dir:   ./shared/

  Share the invite code with collaborators.
  Point your AI agent at this binary as an MCP server.

  ▸ Waiting for peers and agent connection...
```

### `collab-ai join <invite>`

Joins a session as a peer.

```
$ collab-ai join COLLAB-7K2P-9XLM [flags]

Flags:
  --dir string       Directory to sync into (default "./shared")
  --quiet            Suppress non-essential output
```

**Behavior:**

1. Decode invite. If invalid format, error out immediately.
2. Validate `--dir` exists; create if it doesn't.
3. Start Store, Sync Engine, Transport.
4. Call `transport.Join(invite)`. On failure, print error to stderr and exit non-zero.
5. After successful handshake, request initial state (full log + file list) from host. (Implementation detail: send a `hello` with no Token if it's already established, or include a "give me everything since timestamp 0" semantic — figure out at implementation time. Acceptable v1: just receive what comes, expect the host to push initial state on accept.)
6. Start the MCP server on stdio.
7. Block until SIGINT or host disconnects.

**Sample output (to stderr):**

```
collab-ai v0.1.0 — joining session

  Invite:       COLLAB-7K2P-9XLM
  Host:         <host peer info>
  Shared dir:   ./shared/

  ✓ Connected to host.
  ✓ Initial state synced (3 log entries, 7 files).

  ▸ Point your AI agent at this binary as an MCP server.
```

### `collab-ai status`

Prints session info. Useful while a session is running (run from a separate terminal).

This command does NOT start a session; it reads from a running collab-ai instance. v1 implementation:

- The running collab-ai process writes its state to `~/.collab-ai/state.json` periodically.
- `status` reads that file.

```
$ collab-ai status

collab-ai v0.1.0

  Role:           host
  Invite:         COLLAB-7K2P-9XLM
  Started:        2 minutes ago
  Shared dir:     ./shared/
  Log entries:    14
  Files in sync:  7

  Peers:
    self    <peer-id-A>   you (host)
    peer    <peer-id-B>   joined 90s ago
```

If no session is running, print `no active collab-ai session` and exit non-zero.

### `collab-ai export`

Dumps the conversation log of the running session as JSON.

```
$ collab-ai export [flags]

Flags:
  --out string       Output path (default "session-<timestamp>.json")
```

Reads from the same state file as `status`. Output format:

```json
{
  "session_id": "...",
  "started_at": "2026-04-28T10:30:00Z",
  "exported_at": "2026-04-28T11:15:00Z",
  "peers": [
    {"id": "...", "role": "host"},
    {"id": "...", "role": "peer"}
  ],
  "entries": [
    {"id": "...", "timestamp": "...", "peer_id": "...", "role": "user", "content": "..."},
    ...
  ]
}
```

### `collab-ai version`

Prints the version. Trivial.

## Conventions

- All log/status output goes to **stderr** when stdio is occupied by MCP.
- All errors exit non-zero with a descriptive message (no stack traces shown to users).
- `--quiet` reduces but does not eliminate output (errors always show).
- No interactive prompts. v1 is non-interactive; everything is flags.
