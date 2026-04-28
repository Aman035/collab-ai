# collab-ai

> Multiplayer for AI coding agents. Peer-to-peer over [Gensyn Axl][axl].

A small Go CLI that lets two developers share a Claude Code, Codex, or Cursor
session. Each person brings their own agent and their own API key. No central
server, no shared accounts, no platform.

```
┌─ peer A ─────────────┐                       ┌─ peer B ─────────────┐
│  AI agent            │                       │  AI agent            │
│       ↕ MCP (stdio)  │                       │       ↕ MCP (stdio)  │
│  collab-ai           │  ◀──── Axl P2P ────▶  │  collab-ai           │
│  ./shared/ ←─ files  │      log + files      │  ./shared/ ─→ files  │
└──────────────────────┘                       └──────────────────────┘
```

## Install

You'll need two things on your `PATH`: the `collab-ai` binary and Gensyn Axl's
`node` binary (the peer-routing daemon collab-ai talks to).

**collab-ai:**

```bash
# Latest release (recommended once binaries are published)
curl -sSL https://raw.githubusercontent.com/Aman035/collab-ai/main/install.sh | sh

# Or build from source
go install github.com/Aman035/collab-ai/cmd/collab-ai@latest
```

**Gensyn Axl** (one-time, until they publish prebuilt binaries):

```bash
git clone https://github.com/gensyn-ai/axl.git
cd axl && go build -o node ./cmd/node/
# put ./node on your PATH, or set COLLAB_AXL_NODE=/path/to/node
```

Requires Go 1.22+. macOS and Linux on amd64 / arm64 are first-class.

## Quick start

Host a session:

```bash
$ collab-ai host --dir ./shared

  Invite code:
    COLLAB-04d96b03c4...c0c2-7eb45cac54

  Shared dir:  ./shared
  Peer ID:     04d96b03c49ba9f1...d3d539ef

  ▸ Waiting for peers...
```

Share the invite out-of-band (Slack, SMS, carrier pigeon). On the other
machine:

```bash
$ collab-ai join COLLAB-04d96b03c4...c0c2-7eb45cac54 --dir ./shared

  ✓ Connected to host.
  ▸ Ready.
```

Now point your AI agent at the local `collab-ai` binary as an MCP server.
For Claude Code, that's a one-line entry in `~/.claude.json` under
`mcpServers`:

```json
{
  "mcpServers": {
    "collab-ai": { "command": "collab-ai", "args": ["host", "--dir", "./shared"] }
  }
}
```

(Use `["join", "<invite>", "--dir", "./shared"]` on the joiner side.)

From then on, prompts, answers, and file edits in `./shared/` sync across
peers automatically.

## What you can do

- **`collab-ai host`** — start a new session. Prints an invite code.
- **`collab-ai join <invite>`** — connect to a host's session.
- **`collab-ai status`** — from a third terminal, show the running session's
  role, invite, peer table, and log/file counts.
- **`collab-ai export --out session.json`** — dump the conversation log as
  JSON.
- **`collab-ai version`** — print build info.

## Why

AI coding agents are single-player. When two developers want to debug, review,
or pair on a problem together, the options today all break in some important
way:

- **Screen-share is passive.** One person drives, the other watches.
- **Shared accounts break the rules.** Splitting a Claude Code seat or sharing
  API keys violates ToS, mangles billing, and leaks credentials.
- **Async git loses the live reasoning.** By the time the diff lands, the
  chain of thought that produced it is already gone.

collab-ai resolves the tension without a platform: peers connect directly,
each bringing their own agent and their own keys.

## What syncs (and what doesn't)

Only two things cross the network:

1. An **append-only conversation log** (CRDT G-Set keyed by ULID).
2. The contents of **`./shared/`** (per-file LWW-Register, add-wins on
   concurrent edit + delete; whole-file send capped at 256 KB).

Everything else stays local: API keys, files outside `./shared/`, environment
variables, tool-call results, the agent's internal state.

## Status

**v0.1 — early.** Demo-ready for Claude Code with two participants. The
engine layer (store + sync + transport) is unit-tested and has an integration
test that brings up two real Axl daemons and round-trips a log entry in
~200 ms.

Not yet:

- More than 4 simultaneous peers (designed-for, not exercised).
- Persistence across host restarts.
- Multi-machine demos without manual TLS endpoint exchange — pending public
  Gensyn Axl bootstrap peers.
- Verified compatibility with Codex / Cursor (architecture supports them).

See [`docs/09-build-plan.md`](docs/09-build-plan.md) for what's done and
what's next.

## Architecture

Five layers, agent → network:

| # | Component | Responsibility |
|---|-----------|----------------|
| 1 | MCP Server (`internal/mcp`) | bridges the local agent to collab-ai's tools |
| 2 | Session Store (`internal/store`) | G-Set log + LWW-Register file index |
| 3 | Sync Engine (`internal/sync`) | fsnotify watcher + echo-suppressed broadcast |
| 4 | Axl Transport (`internal/transport`) | spawns the `node` daemon; HTTP client over `localhost:9002` |
| 5 | `./shared/` | the collaboration sandbox |

The transport layer is **replaceable** — no Axl-specific types cross the
package boundary. See [`docs/02-architecture.md`](docs/02-architecture.md).

## Contributing

This is a hackathon-scale project still finding its shape. Issues and PRs
welcome; large changes, please open an issue first to talk it through.

## License

[MIT](LICENSE)

[axl]: https://github.com/gensyn-ai/axl
