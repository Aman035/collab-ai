# collab-ai

> Multiplayer for AI coding agents. Peer-to-peer over Gensyn Axl.

A small CLI that lets two developers share a Claude Code, Codex, or Cursor session. Each person brings their own agent and their own API key. No central server, no shared accounts, no platform.

## Install

```bash
curl -sSL https://collab-ai.dev/install.sh | sh
```

Or:

```bash
brew install collab-ai/tap/collab-ai
go install github.com/Aman035/colabAI/cmd/collab-ai@latest
```

## Quick start

Host a session:

```bash
collab-ai host --dir ./project
# → COLLAB-7K2P-9XLM
```

Join from another machine:

```bash
collab-ai join COLLAB-7K2P-9XLM --dir ./project
```

Both participants point their AI coding agent at the local `collab-ai` MCP server. From then on, prompts, answers, and file edits sync across peers automatically.

## Why

AI coding agents are single-player. When two developers want to debug, review, or pair on a problem together, the options today are screen-sharing (passive), sharing accounts (insecure), or working separately and merging in git (loses the live reasoning). collab-ai makes it possible to share an AI coding session without sharing accounts and without a central server.

## How it works

```
┌─ peer A ─────────────┐                        ┌─ peer B ─────────────┐
│  AI agent            │                        │  AI agent            │
│       ↕ MCP (stdio)  │                        │       ↕ MCP (stdio)  │
│  collab-ai           │  ◀──── Axl P2P ────▶   │  collab-ai           │
│  ./shared/ ←─ files  │      log + files       │  ./shared/ ─→ files  │
└──────────────────────┘                        └──────────────────────┘
```

Each participant runs `collab-ai` locally. The binary speaks MCP to the agent (so the agent can read the shared conversation log and list shared files) and connects peer-to-peer to other participants via Gensyn Axl. Two things sync between peers: an append-only conversation log and the contents of a designated shared working directory.

## Status

v0.1 — early. Demo-ready for Claude Code with two participants. See `docs/` for the full design.

## License

MIT
