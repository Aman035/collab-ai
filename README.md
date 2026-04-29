# collab-ai

> Multiplayer for AI coding agents. Peer-to-peer over [Gensyn Axl][axl].

A small Go CLI that lets two developers share a Claude Code, Codex, or
Cursor session. Each person brings their own agent and their own API key.
No central server, no shared accounts, no platform.

```
┌─ peer A ─────────────┐                       ┌─ peer B ─────────────┐
│  AI agent            │                       │  AI agent            │
│       ↕ MCP          │                       │       ↕ MCP          │
│  collab              │  ◀──── Axl P2P ────▶  │  collab              │
│  ./shared/ ←─ files  │      log + files      │  ./shared/ ─→ files  │
└──────────────────────┘                       └──────────────────────┘
```

## Install

One command. Installs the `collab` binary on your PATH; the Gensyn Axl
daemon is auto-built into `~/.collab/bin/axl-node` on first session.

```bash
curl -sSL https://raw.githubusercontent.com/Aman035/collab-ai/main/install.sh | sh
```

Or build from source:

```bash
go install github.com/Aman035/collab-ai/cmd/collab@latest
```

Requires Go 1.22+ and `git` (the latter is needed once, the first time `collab`
fetches Axl).

## Use

**Host a session:**

```bash
collab create
```

A TUI opens with your auto-generated session name (`sharp-cheetah`),
your handle (`clever-otter`), and an invite code. Press `c` to copy
the invite, `a` to launch your AI agent, `q` to quit.

**Join from another machine (or another tab):**

```bash
collab join COLLAB-paste-the-invite-here
```

Same TUI, your own handle, no invite to share. Press `a` and the agent
opens already aware of the session — `get_shared_log` returns the
existing context, `list_shared_files` reports anything in `./shared/`.

**Other verbs:**

- `collab status` — peers + log/file counts for the running session.
- `collab export --out session.json` — dump the conversation log.
- `collab help <verb>` — detailed help for any subcommand.
- `collab version` — build info.

## What syncs

Two things cross the network:

1. **Shared conversation log** — append-only G-Set keyed by ULID. Every
   `post_to_log` from any peer's agent shows up via `get_shared_log` on
   every other peer's agent.
2. **`./shared/`** — per-file LWW-Register CRDT, add-wins on concurrent
   edit + delete. Whole-file send capped at 256 KB.

Everything else stays local: API keys, files outside `./shared/`,
environment, tool-call results, the agent's internal state.

## Why

AI coding agents are single-player. When two developers want to debug,
review, or pair on a problem together, the options today all break:

- **Screen-share is passive.** One person drives, the other watches.
- **Shared accounts break the rules.** Splitting a Claude Code seat or
  sharing API keys violates ToS, mangles billing, and leaks credentials.
- **Async git loses the live reasoning.** By the time the diff lands,
  the chain of thought that produced it is already gone.

`collab` resolves the tension without a platform: peers connect directly,
each bringing their own agent and their own keys.

## Architecture

Five layers, agent → network:

| # | Component | Responsibility |
|---|-----------|----------------|
| 1 | MCP Server (`internal/mcp`) | bridges the local agent to collab's tools (HTTP or stdio) |
| 2 | Session Store (`internal/store`) | G-Set log + LWW-Register file index |
| 3 | Sync Engine (`internal/sync`) | fsnotify watcher, echo-suppressed broadcast, replay-on-join |
| 4 | Axl Transport (`internal/transport`) | spawns the Axl daemon; HTTP client over `localhost:9002` |
| 5 | `./shared/` | the collaboration sandbox |

`internal/bootstrap` handles first-time install of `axl-node` into
`~/.collab/bin/`. `internal/tui` is the bubbletea session view.

The transport layer is **replaceable** — no Axl-specific types cross the
package boundary. See [`docs/02-architecture.md`](docs/02-architecture.md).

## Status

**v0.2 — early.** Demo-ready for Claude Code with two participants on the
same machine. Pre-built binaries arrive with the first tagged release;
in the meantime use `go install`.

Not yet:

- Multi-machine demos without manual TLS endpoint exchange — pending
  public Gensyn bootstrap peers.
- More than 4 simultaneous peers (designed-for, not exercised).
- Persistence across host restarts.
- Verified compatibility with Codex / Cursor (architecture supports them).

See [`docs/09-build-plan.md`](docs/09-build-plan.md) for what's done and
what's next.

## License

[MIT](LICENSE)

[axl]: https://github.com/gensyn-ai/axl
