<div align="center">

# collab

**Multiplayer for AI coding agents.** Peer-to-peer over [Gensyn Axl][axl].

Pair on Claude Code (or any MCP-capable agent) across machines, across
networks, with no hosted platform. Each developer brings their own
agent and their own API key.

[**aman035.github.io/collab-ai**](https://aman035.github.io/collab-ai/) &nbsp;·&nbsp;
[Install](#install) &nbsp;·&nbsp;
[Use](#use) &nbsp;·&nbsp;
[How it works](#how-it-works) &nbsp;·&nbsp;
[Architecture](#architecture)

[![release](https://img.shields.io/github/v/release/Aman035/collab-ai?color=2547ff&label=release)](https://github.com/Aman035/collab-ai/releases)
[![license](https://img.shields.io/github/license/Aman035/collab-ai?color=2547ff)](LICENSE)

</div>

---

## What it is

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="web/architecture-dark.svg">
    <img src="web/architecture.svg" alt="Two peers, each running an AI agent connected over MCP to a local collab binary, peered to the other via Axl P2P" width="900">
  </picture>
</p>

Two channels cross the network and nothing else:

- **Shared conversation log** (G-Set CRDT keyed by ULID). Every
  `post_to_log` from any peer's agent shows up via `get_shared_log`
  on every other peer's agent. `ask_peer` and `respond_to_peer` add
  direct, targeted requests that one agent makes of another.
- **`./shared/`**. Files dropped here propagate via fsnotify, merged
  with a per-file LWW-Register CRDT (add-wins on concurrent edit + delete),
  capped at 256 KB per file.

Everything else stays local: API keys, files outside `./shared/`,
environment variables, your agent's tool-call results.

## Install

One curl. The Gensyn Axl daemon auto-builds into `~/.collab/bin/` on
first session, so there is no second binary to chase.

```bash
curl -sSL https://raw.githubusercontent.com/Aman035/collab-ai/main/install.sh | sh
```

Or build from source:

```bash
go install github.com/Aman035/collab-ai/cmd/collab@latest
```

Requires Go 1.22+ and `git` (used once, on the first Axl bootstrap).
Prebuilt binaries for darwin / linux × amd64 / arm64 ship with every
[release](https://github.com/Aman035/collab-ai/releases).

## Use

### Single machine

Two terminal tabs. Tab A:

```bash
collab create alice
```

Copy the invite from the TUI. Tab B:

```bash
collab join COLLAB-...  bob
```

Both TUIs show each other in the peer roster. Press `[a]` in either to
launch Claude. Ask Claude to `post_to_log "hello"` and the other tab's
Claude sees it via `get_shared_log`.

### Two machines (e.g. SSH)

On the host server:

```bash
collab create alice --public-addr tls://server1.example.com:9001
```

The invite embeds that address. On the joiner server:

```bash
collab join COLLAB-...  bob
```

The joiner's Axl daemon dials `server1.example.com:9001` automatically.
Make sure port 9001 on the host is reachable from the joiner (security
group or firewall rule).

### What the TUI looks like

```
╭───────────────────────────────────────────────────────────────────────────────╮
│  collab  ·  sleek-platypus  ·  host                          you are alice   │
│                                                                               │
│  ▎ INVITE                                                                     │
│    COLLAB-eae0761a…6c61-5768dd24ea-dGxzOi8vc2Vy…                              │
│      press [c] to copy                                                        │
│                                                                               │
│  ▎ PEERS (2)                       ▎ LOG (3)                                  │
│    ●  alice  · you                   14:32  bob: cache.go has off-by-one      │
│       writing the test harness       14:33  alice ?→bob: review widget.go?    │
│    ●  bob    · joined 4m ago         14:33  bob ←answer: lgtm, ship it        │
│       reviewing widget.go                                                     │
│                                                                               │
│  ▎ SHARED FILES (2)                                                           │
│    cache.go    2.4KB  · from bob · 30s ago                                    │
│    widget.go   1.1KB  · from alice · 12s ago                                  │
│                                                                               │
│  [a] launch claude    [c] copy invite    [j/k] scroll log    [q] quit         │
╰───────────────────────────────────────────────────────────────────────────────╯
```

## What the agent can do

Five MCP tools land in the launched Claude session, auto-discovered via
the per-session `CLAUDE.md` collab writes for it:

| Tool | What it does |
|---|---|
| `get_shared_log` | Read every entry: your own posts, peers' posts, plus `ask_peer` questions and answers. Call at session start to inherit context. |
| `post_to_log` | Broadcast a message to every peer's agent. |
| `ask_peer(target_peer, question)` | Direct a question at one peer's agent. They see it tagged in their log and respond with `respond_to_peer`. |
| `respond_to_peer(question_id, answer)` | Answer a question another peer's agent asked you. |
| `set_status(status)` | Update your "what I'm doing" line. Appears under your handle in everyone's TUI. |
| `list_shared_files` | What's in `./shared/` right now. |

## Verbs

```
collab create [name] [--public-addr ...] [--listen ...]   start a session
collab join <invite> [name]                                join one
collab status                                              peers + counts (read-only)
collab export --out file.json                              dump the log as JSON
collab help [verb]                                         per-command help
collab version                                             build info
```

## How it works

When you `collab create`:

1. Allocate a session dir at `~/collab/<auto-name>/`. The agent runs
   here; `./shared/` is the synced subdir.
2. Spawn the Axl daemon (auto-built from source on first run into
   `~/.collab/bin/axl-node`). Listens on `tls://0.0.0.0:9001` by default.
3. Mint an invite: `COLLAB-<peer_id>-<token>-<addr_b64>`.
4. Start an HTTP MCP server on a random local port. collab is the
   parent, the agent is the child.
5. Register the MCP server at user-scope via `claude mcp add`. No
   per-project approval prompt.
6. Write `CLAUDE.md` into the session dir so the agent boots aware of
   the available tools and when to use them.
7. Open the TUI. Press `[a]` and Claude launches with cwd = session dir.

## Architecture

Five layers, agent to network:

| # | Component | Responsibility |
|---|-----------|----------------|
| 1 | MCP server (`internal/mcp`) | bridges the local agent to collab's tools (HTTP or stdio) |
| 2 | Session store (`internal/store`) | G-Set log + LWW-Register file index, tombstones, change subscriptions |
| 3 | Sync engine (`internal/sync`) | fsnotify watcher, echo-suppressed broadcast, replay-on-join |
| 4 | Axl transport (`internal/transport`) | spawns the Axl daemon; HTTP client over `localhost:NNNN` |
| 5 | `./shared/` | the collaboration sandbox |

Plus `internal/tui` (bubbletea), `internal/handle` (fun-name
generator), `internal/bootstrap` (first-time Axl install), and
`internal/state` (the `~/.collab/state.json` snapshot that powers
`collab status`).

The transport layer is replaceable. No Axl-specific types cross its
package boundary. See [`docs/02-architecture.md`](docs/02-architecture.md)
for the full breakdown.

## Status

Demo-ready for Claude Code with two participants on the same machine
or across two reachable hosts. Engine layer (store + sync + transport)
is unit-tested and has integration tests that bring up real Axl
daemons and round-trip a log entry in around 200ms.

Not yet:

- More than 4 simultaneous peers (architecture supports them, not
  exercised in tests).
- Persistence across host restarts (sessions are ephemeral).
- Public Gensyn bootstrap nodes, which would let two machines pair
  without the host exposing a port. Today, you set `--public-addr`
  to a reachable endpoint.
- Verified Codex / Cursor compatibility (architecture is
  agent-agnostic; testing pending).

See [`docs/09-build-plan.md`](docs/09-build-plan.md) for the full plan
and what's done.

## Links

- Website: <https://aman035.github.io/collab-ai/>
- Releases: <https://github.com/Aman035/collab-ai/releases>
- Built on Gensyn Axl: <https://github.com/gensyn-ai/axl>

## License

[MIT](LICENSE)

[axl]: https://github.com/gensyn-ai/axl
