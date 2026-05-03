<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="web/banner-dark.svg">
    <img src="web/banner.svg" alt="collab: multiplayer for AI coding agents" width="100%">
  </picture>
</p>

<p align="center">
  <a href="https://aman035.github.io/collab-ai/"><strong>Website</strong></a>
  &nbsp;·&nbsp;
  <a href="#install">Install</a>
  &nbsp;·&nbsp;
  <a href="#how-to-use">How to use</a>
  &nbsp;·&nbsp;
  <a href="#architecture">Architecture</a>
</p>

<p align="center">
  <a href="https://github.com/Aman035/collab-ai/releases"><img src="https://img.shields.io/github/v/release/Aman035/collab-ai?color=0d9488&label=release" alt="release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Aman035/collab-ai?color=0d9488" alt="license"></a>
</p>

---

## Problem Statement

AI coding agents are single-player. Real engineering (debugging, code
review, mentorship, incident response) is rarely solo work. The current
options for two developers who both want to use AI on the same problem
all break in some important way:

1. **Screen-share is passive.** One person drives, the other watches. The
   observer can't ask their own question without interrupting the driver's
   flow, and only one agent is doing real work.
2. **Shared accounts break the rules.** Splitting a Claude Code seat or
   sharing API keys violates ToS, mangles billing, and leaks credentials.
3. **Async git loses the live reasoning.** By the time the diff lands in
   a PR, the chain of thought that produced it is already gone.
4. **Pasting transcripts into Slack** goes stale by the time anyone reads
   it, and a wall of agent text is unreadable in chat.

The deeper tension: developers want **shared context** (everyone sees the
same problem and code) but need **independent agency** (each person asks
their own questions, uses their own quota). Centralized solutions require
a hosted platform, which means lock-in, privacy concerns, and platform
risk.

## Solution & Use Cases

`collab` is a small Go CLI that gives every developer their own AI agent
plus a shared pair-programming channel between agents. Peer-to-peer over
[Gensyn Axl][axl]. No central server, no shared accounts.

Two channels cross the wire and nothing else:

- **A shared conversation log** (G-Set CRDT, ULID-keyed). Every
  `post_to_log` from any peer's agent appears in every other peer's
  `get_shared_log`. `ask_peer` and `respond_to_peer` add direct, targeted
  requests one agent makes of another.
- **A shared `./shared/` directory.** Files dropped here propagate via
  fsnotify, merged with a per-file LWW-Register CRDT (add-wins on
  concurrent edit + delete, capped at 256 KB per file).

Everything else stays local: API keys, files outside `./shared/`,
environment, your agent's tool-call results.

<p align="center">
  <img src="web/flow.svg" alt="Animated diagram: posts, ask_peer requests, answers, and files travel between peer alice and peer bob over Axl on a 12-second loop" width="900">
</p>

**Concrete use cases:**

- **Two-developer debugging across machines.** Senior + junior hit a prod
  bug. Both join the session, each agent explores a different part of the
  codebase, findings get posted to the shared log, and the diff lands in
  `./shared/`.
- **Live code review with two agents.** Reviewer's agent reads the diff
  and posts comments to the log. Author's agent reads them, addresses
  them, posts back. Files and proposed fixes flow through `./shared/`.
- **Async handoff across timezones.** Developer A works during their day,
  posting context + decisions + scratch files. Developer B wakes up,
  joins the session, and their agent inherits the full backlog via
  initial-state replay (not a stale PR + Slack thread).
- **Mixed-agent coordination.** One developer on Claude Code, another on
  Codex, third on Cursor. The protocol is agent-agnostic; collab is the
  wire between any MCP-capable agents.
- **Incident response.** Three on-call engineers in one session, three
  agents exploring three hypotheses simultaneously. First one to find the
  root cause posts; everyone's context updates.
- **Mentorship.** Instructor's agent does real work; student's agent
  handles basics; student watches the senior reasoning unfold via
  `get_shared_log` rather than over a Zoom screen-share.

## Architecture

Inside one peer, the work is split across five small Go packages. Each
layer talks to the layer immediately below it through a Go interface, so
the transport (and the agent it bridges to) is swappable.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="web/architecture-stack-dark.svg">
    <img src="web/architecture-stack.svg" alt="Vertical stack of layers inside one peer: AI agent on top, internal/mcp, internal/store + internal/sync, internal/transport, and the Gensyn Axl daemon at the bottom routing onto the Yggdrasil mesh" width="640">
  </picture>
</p>

## Install

One curl. The Gensyn Axl daemon auto-builds into `~/.collab/bin/` on the
first session, so there's no second binary to chase.

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

## How to Use

Two developers, two laptops, somewhere on the same network or both on
the public internet. Run `collab` on each side. The host opens a port,
the joiner dials it, and both agents share a session over the wire.

### 1. Host opens a session

On your machine, pick a hostname or IP that your pair partner can
actually reach you on (your laptop's hostname on the corporate VPN,
your office IP, or a Tailscale / cloud-instance address):

```bash
collab create alice --public-addr tls://your-host.example.com:9001
```

Make sure port `9001` is reachable from your partner's machine.
Firewall, security-group, or port-forward as needed.

The TUI opens with the session name, your handle, and an invite code.
Press `[c]` to copy the invite, then send it to your partner over any
out-of-band channel (Slack, SMS, whatever).

### 2. Partner joins

On their machine:

```bash
collab join COLLAB-...  bob
```

The joiner's Axl daemon dials your `--public-addr` automatically using
the address baked into the invite. Within a second or two, both peer
rosters show each other.

### 3. Launch agents and pair

In each TUI, press `[a]` to launch Claude Code (or any other agent
you've installed). The agent opens already aware of the session: it
runs `get_shared_log` first to inherit context, then uses `post_to_log`,
`ask_peer`, `respond_to_peer`, and `set_status` proactively as you and
your partner work.

Drop files into your local `~/collab/<session>/shared/` and they
propagate to the partner's `./shared/` within a few seconds. Files
anywhere else stay private.

When you're done, `[q]` or `Ctrl+C` tears down the session cleanly.

### What the agent can do

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

### Verbs

```
collab create [name] [--public-addr ...] [--listen ...]   start a session
collab join <invite> [name]                                join one
collab status                                              peers + counts (read-only)
collab export --out file.json                              dump the log as JSON
collab help [verb]                                         per-command help
collab version                                             build info
```

---

[MIT](LICENSE).  Built on [Gensyn Axl][axl].

[axl]: https://github.com/gensyn-ai/axl
