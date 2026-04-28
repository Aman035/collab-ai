# 02 · Architecture

## System view

Two or more participants. Each runs a local `collab-ai` binary. Each participant's AI coding agent connects to its own local `collab-ai` over MCP/stdio. The `collab-ai` binaries connect to each other peer-to-peer via Gensyn Axl.

```
┌─────────────────── Participant A (Host) ────────────────────┐  ┌─────────────────── Participant B (Joiner) ──────────────────┐
│                                                             │  │                                                             │
│  ┌───────────────────────────────────────────────────────┐  │  │  ┌───────────────────────────────────────────────────────┐  │
│  │   AI Coding Agent  (Claude Code / Codex / Cursor)     │  │  │  │   AI Coding Agent  (Claude Code / Codex / Cursor)     │  │
│  │   own API key — own quota                             │  │  │  │   own API key — own quota                             │  │
│  └────────────────────────┬──────────────────────────────┘  │  │  └────────────────────────┬──────────────────────────────┘  │
│                           │ MCP (stdio)                     │  │                           │ MCP (stdio)                     │
│  ┌────────────────────────▼──────────────────────────────┐  │  │  ┌────────────────────────▼──────────────────────────────┐  │
│  │   collab-ai  (Go process)                             │  │  │  │   collab-ai  (Go process)                             │  │
│  │   ┌─────────────────────────────────────────────┐     │  │  │  │   ┌─────────────────────────────────────────────┐     │  │
│  │   │ MCP Server                                  │     │  │  │  │   │ MCP Server                                  │     │  │
│  │   │   get_shared_log · post_to_log · list_files │     │  │  │  │   │ get_shared_log · post_to_log · list_files   │     │  │
│  │   └─────────────────────┬───────────────────────┘     │  │  │  │   └─────────────────────┬───────────────────────┘     │  │
│  │   ┌─────────────────────▼───────────────────────┐     │  │  │  │   ┌─────────────────────▼───────────────────────┐     │  │
│  │   │ Session State Store                         │     │  │  │  │   │ Session State Store                         │     │  │
│  │   │   append-only log · file index              │     │  │  │  │   │ append-only log · file index                │     │  │
│  │   └─────────────────────┬───────────────────────┘     │  │  │  │   └─────────────────────┬───────────────────────┘     │  │
│  │   ┌─────────────────────▼───────────────────────┐     │  │  │  │   ┌─────────────────────▼───────────────────────┐     │  │
│  │   │ Sync Engine                                 │     │  │  │  │   │ Sync Engine                                 │     │  │
│  │   │   fsnotify · broadcast · merge (CRDT)        │     │  │  │  │   │ fsnotify · broadcast · merge (CRDT)          │     │  │
│  │   └─────────────────────┬───────────────────────┘     │  │  │  │   └─────────────────────┬───────────────────────┘     │  │
│  │   ┌─────────────────────▼───────────────────────┐     │  │  │  │   ┌─────────────────────▼───────────────────────┐     │  │
│  │   │ Gensyn Axl P2P Transport                    │◄────┼──┼──┼──┼──►│ Gensyn Axl P2P Transport                    │     │  │
│  │   └─────────────────────────────────────────────┘     │  │  │  │   └─────────────────────────────────────────────┘     │  │
│  └───────────────────────────────────────────────────────┘  │  │  └───────────────────────────────────────────────────────┘  │
│                                                             │  │                                                             │
│   ./shared/  (working directory, file-watched)              │  │   ./shared/  (working directory, file-watched)              │
└─────────────────────────────────────────────────────────────┘  └─────────────────────────────────────────────────────────────┘
```

## Five layers, in order from agent to network

| # | Component | Responsibility | Spec |
|---|-----------|----------------|------|
| 1 | **MCP Server** | Bridge between the AI agent and the rest of collab-ai. Exposes 3 tools. | [03](03-component-mcp-server.md) |
| 2 | **Session State Store** | Single source of truth for what's in the session locally. Append-only log + file index. | [04](04-component-store.md) |
| 3 | **Sync Engine** | Watches local store + filesystem; broadcasts changes; applies inbound changes. | [05](05-component-sync-engine.md) |
| 4 | **Axl Transport** | Peer discovery, connection, byte-level message passing. | [06](06-component-transport.md) |
| 5 | **./shared/ working dir** | The collaboration sandbox. A normal directory the agent treats as its working dir. | (no separate spec — see overview here) |

The transport layer is **replaceable**. Don't leak Axl-specific types past `internal/transport/`. Use `pkg/protocol` types at every boundary above the transport.

The MCP layer is the **only** integration point with the AI agent. If an agent speaks MCP, it works with collab-ai. This is what makes the system agent-agnostic.

## Data flow — single turn

What happens when participant A types a prompt:

```
1. A types a prompt into their AI agent
2. A's agent calls MCP tool: get_shared_log()
   → MCP Server reads from local Store
   → returns full conversation history (including B's earlier turns)
3. A's agent reasons with the full shared context, generates a response,
   optionally edits files in ./shared/
4. A's agent calls MCP tool: post_to_log({role: "assistant", content: "..."})
   → MCP Server writes to local Store
   → Store emits a Change event
5. Sync Engine (on A) receives the Change, serializes it as a WireMessage,
   pushes it to Axl Transport
6. A's Axl Transport sends the WireMessage to all connected peers
7. B's Axl Transport receives the WireMessage, hands it to B's Sync Engine
8. B's Sync Engine deserializes, applies to B's local Store
9. On B's next turn, B's agent calls get_shared_log() and sees A's contribution
```

File edits follow a parallel path:

```
1. A's agent edits a file in ./shared/
2. fsnotify (in A's Sync Engine) fires a write event
3. Sync Engine reads the file, hashes it, updates Store with new FileMeta
4. Sync Engine builds a FileChunk WireMessage and broadcasts it
5. B's Sync Engine hands the FileMeta to B's Store, which runs the per-file LWW-Register merge with `(ModTime, PeerID)` tiebreak; if the merge accepts, the Sync Engine writes the bytes to B's ./shared/
6. B's agent sees the new file the next time it lists ./shared/
```

## What flows over the network

Only two payload kinds exist on the wire:

- **`log_entry`** — a new conversation log entry from a peer
- **`file_chunk`** — a file added, changed, or deleted in ./shared/

Plus two control messages:

- **`hello`** — sent on connect; carries peer ID and session metadata
- **`goodbye`** — sent before clean disconnect

Everything else (tool-call results, agent internal state, what model the agent is using, the agent's API key) stays local. **Nothing else crosses the network.**

See `docs/07-protocol.md` for exact message schemas.

## Boundary discipline

This is the most important architectural rule:

```
┌───────────────────────────────────────┐
│  AI Agent  (third-party)              │
└────────────────┬──────────────────────┘
                 │ MCP boundary
┌────────────────▼──────────────────────┐
│  internal/mcp                         │
└────────────────┬──────────────────────┘
                 │ Store interface
┌────────────────▼──────────────────────┐
│  internal/store                       │
└────────────────┬──────────────────────┘
                 │ Store.Subscribe() events
┌────────────────▼──────────────────────┐
│  internal/sync                        │
└────────────────┬──────────────────────┘
                 │ Transport interface  ←── pkg/protocol types only
┌────────────────▼──────────────────────┐
│  internal/transport                   │
└────────────────┬──────────────────────┘
                 │ HTTP (localhost:9002) + os/exec
┌────────────────▼──────────────────────┐
│  Axl `node` daemon  (child process)   │
└───────────────────────────────────────┘
```

Each layer talks to the layer immediately below through a Go interface. The `pkg/protocol` package defines the wire types and is the only package shared across the boundary above the transport.

If you find yourself importing Axl types in `internal/sync`, stop and refactor. The transport must be replaceable.
