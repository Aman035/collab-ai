# 01 · Overview

## What

A Go CLI named `collab-ai` that turns single-player AI coding agents (Claude Code, Codex, Cursor) into multiplayer ones. Each developer keeps their own agent and their own API key. The CLI runs alongside the agent, exposes itself as an MCP server, and connects peer-to-peer to other developers' CLIs over Gensyn Axl.

## Why

AI coding agents are designed for one developer, one terminal, one conversation. Real engineering — debugging, code review, pairing, mentorship — is rarely solo work. The current workarounds all break in some important way:

- **Screen-sharing**: one person passive, can't ask their own questions
- **Shared accounts**: insecure, breaks billing, ToS violation
- **Work separately, merge in git**: loses the live reasoning that makes pairing valuable
- **Paste transcripts after the fact**: stale by the time anyone reads them

The tension: developers want **shared context** (everyone sees the same problem and code) but need **independent agency** (each person asks their own questions, uses their own quota).

Centralized solutions require a platform — someone hosts the state, brokers auth, manages cross-user billing — which means lock-in, privacy concerns, and platform risk. collab-ai resolves the tension without a platform: peers connect directly, each bringing their own agent and their own keys.

## Use cases

- **Two-person debugging.** Senior + junior engineer hit a prod bug; both join the session; their agents explore different parts of the codebase in parallel.
- **Live code review.** Reviewer joins the author's session before approving; both agents work on the same diff.
- **Incident response.** Three on-call engineers in one session, three agents exploring three hypotheses simultaneously.
- **Mentorship.** Student joins instructor's session; instructor's agent does real work; student's agent handles basics; student watches reasoning unfold.
- **Mixed-agent sessions.** One developer on Claude Code, another on Codex, same session — the protocol is agent-agnostic.

## What "good" looks like (v1)

A two-person demo where:

1. Setup takes under 60 seconds (one command per side, plus an invite code shared out-of-band). Assumes the Axl `node` binary is on PATH — `collab-ai host` / `join` spawn it as a child process automatically. First-time install of `node` is a separate ~one-minute `git clone` + `go build`, documented as a prerequisite.
2. Conversation log syncs in under 1 second.
3. File changes in `./shared/` propagate in a few seconds.
4. Disconnects are graceful — when one peer drops, the others continue.
5. Boundaries are obvious — only `./shared/` and the conversation log are shared; everything else stays local.

## What's NOT in scope for v1

These are real future work; they are not v1 problems and you should not solve them:

- More than 4 simultaneous participants
- Authentication beyond invite tokens
- Persistent sessions across host restarts
- Op-based or character-level text CRDTs (Automerge / Yjs-style). v1 uses simple state-based CRDTs only: G-Set for the log, per-file LWW-Register for file contents, add-wins for the file set
- Partial-file sync — v1 sends whole files, capped at 256 KB
- Web UI
- Agents beyond Claude Code (architecture supports them; testing comes later)

## Three success lenses

| Lens | Bar |
|------|-----|
| **Hackathon (demo day)** | Two laptops, two agents, working session. Three-minute pitch lands. |
| **Project (1 month)** | 100+ stars. Codex or Cursor compatibility verified. Three working install paths. |
| **User (per session)** | Setup under 60s. Both participants productive. Latency feels live. |
