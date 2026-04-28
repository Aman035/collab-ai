# collab-ai

Multiplayer for AI coding agents. A small Go CLI that lets multiple developers share an AI coding session — Claude Code, Codex, Cursor, or any MCP-capable agent — peer-to-peer over Gensyn Axl. No central server, no shared accounts.

## What you're building

A single Go binary, `collab-ai`, that:

1. Exposes itself as an **MCP server** to a local AI coding agent (Claude Code in v1).
2. Connects **peer-to-peer via Gensyn Axl** to other participants running the same binary.
3. Syncs a **shared conversation log** + a **shared working directory** between peers.

Each participant brings their own agent and their own API key. Nothing is hosted.

## Quick architecture

```
┌─ peer A ─────────────┐                        ┌─ peer B ─────────────┐
│  AI agent            │                        │  AI agent            │
│       ↕ MCP (stdio)  │                        │       ↕ MCP (stdio)  │
│  collab-ai           │  ◀──── Axl P2P ────▶   │  collab-ai           │
│  ./shared/ ←─ files  │      log + files       │  ./shared/ ─→ files  │
└──────────────────────┘                        └──────────────────────┘
```

The binary has 5 layers: **MCP Server → Session State Store → Sync Engine → Axl Transport → ./shared/ working directory**.

See `docs/02-architecture.md` for the full picture.

## How to use this repo

**For any task involving the design**, read `docs/01-overview.md` and `docs/02-architecture.md` first. They are short.

**For implementing a specific component**, read its spec doc:

- MCP Server → `docs/03-component-mcp-server.md`
- Session State Store → `docs/04-component-store.md`
- Sync Engine → `docs/05-component-sync-engine.md`
- Axl Transport → `docs/06-component-transport.md`

**For wire format and inter-peer protocol**, read `docs/07-protocol.md`.

**For the CLI surface (commands, flags, output)**, read `docs/08-cli.md`.

**Before starting work**, read `docs/09-build-plan.md`. It defines the milestones and the order. **Do not skip ahead.** Milestone 1 must work end-to-end before Milestone 2 starts.

**For Go style and conventions specific to this project**, read `docs/10-conventions.md`.

## Stack

- **Language**: Go 1.22+
- **MCP server**: `github.com/mark3labs/mcp-go` (verify current best Go MCP SDK before committing)
- **File watching**: `github.com/fsnotify/fsnotify`
- **CLI framework**: `github.com/spf13/cobra`
- **P2P transport**: Gensyn Axl, accessed as a child-process daemon over its HTTP API on `localhost:9002`. There is no Go SDK; `internal/transport` is a `net/http` client plus an `os/exec` supervisor. M0 verified this end-to-end (see `experiments/axl-hello/`).
- **Build/release**: goreleaser
- **IDs**: ULIDs for log entries (sortable, dedupable)

## Repo layout (target)

```
collab-ai/
├── cmd/
│   └── collab-ai/        # cobra entry point
├── internal/
│   ├── mcp/              # MCP Server (4.1)
│   ├── store/            # Session State Store (4.2)
│   ├── sync/             # Sync Engine (4.3)
│   ├── transport/        # Axl Transport (4.4)
│   └── shareddir/        # ./shared/ helpers (4.5)
├── pkg/
│   └── protocol/         # wire types, shared between peers
├── docs/                 # specs (this directory)
├── CLAUDE.md             # this file
├── README.md             # public readme
├── go.mod
└── .goreleaser.yml
```

`internal/` is for component code that no external consumer should import. `pkg/protocol/` is intentionally public — if someone else wants to write a compatible peer in another language, they read that package.

## Project values

- **Ship the demo first.** Two laptops on the same Wi-Fi sharing a log + shared directory is the bar. Polish comes after.
- **Use the smallest CRDT that gets the job done.** Log = G-Set keyed by ULID. File contents = per-file LWW-Register with `(timestamp, peer_id)` tiebreak. File set = add-wins (a concurrent edit beats a concurrent delete). No op-based CRDTs, no character-level merging in v1.
- **The transport layer is replaceable.** Don't leak Axl-specific types past `internal/transport/`. Use `pkg/protocol` types at boundaries.
- **No persistence in v1.** Sessions live in memory and die with the host. The export command dumps the log if needed.
- **Refuse to grow scope.** If a feature isn't in `docs/09-build-plan.md` for the current milestone, it doesn't get built.

## Out of scope (do not build)

- More than 4 simultaneous peers
- Authentication beyond invite tokens
- Persistence across host restarts
- Web UI
- Cross-peer shell command syncing (each peer runs commands locally only)
- Op-based or character-level text CRDTs (Automerge / Yjs-style) — out of scope for v1; the per-file LWW-Register is the only CRDT we use on file contents

## How sessions work, in one paragraph

The host runs `collab-ai host --dir ./project`, which prints an invite code. The host's binary starts an MCP server on stdio and listens for Axl peers. Joiners run `collab-ai join <invite-code> --dir ./project`, which connects to the host over Axl and syncs the initial state. Each participant points their AI agent (Claude Code etc.) at the local `collab-ai` binary as an MCP server. From then on, prompts and answers flow into a shared append-only log; file edits in `./shared/` propagate to all peers. When the host disconnects, the session ends.
