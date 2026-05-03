# collab: hackathon submission

## Short description (max 100 chars)

Multiplayer for AI coding agents. Two devs, two agents, one shared session. P2P, no central server.

(99 characters)

Alternates if you want to swap:

- Pair on Claude Code, Codex, or Cursor peer-to-peer. Each dev brings their own agent. No platform. (97)
- Multiplayer for Claude Code. Two devs, two agents, one session, peer-to-peer, no shared accounts. (98)


## Description (min 280 chars)

**The problem.** AI coding agents are single-player. Real engineering (debugging, code review, mentorship, incident response) is rarely solo work, but the current options for two developers who both want to use AI on the same problem all break in some important way. Screen-share is passive: one person drives, the other watches, only one agent is doing real work. Sharing a Claude Code seat or splitting an API key violates ToS, mangles billing, and leaks credentials. Async git loses the live reasoning by the time the diff lands in a PR.

**What collab is.** collab is a small Go CLI that turns single-player AI coding into multiplayer. Two developers each launch their own Claude Code (or Codex, or Cursor) and join the same collab session over the public internet, peer-to-peer. Each developer keeps their own agent, their own API key, their own quota. There is no central server, no shared account, no hosted platform.

**How it solves it.** The two agents share exactly two things: a conversation log and a working directory. Posts, ask_peer questions, respond_to_peer answers, and file edits propagate live between peers via a CRDT layer that rides on Gensyn Axl's P2P transport. Both agents see the same context, but each one acts under its own developer's identity and quota. Agents collaborate with each other (asking, answering, splitting reasoning across two heads) instead of one developer passively watching a screen-share. Developers get shared context with independent agency, which is the deeper tension that hosted "team plans" never resolve cleanly.


## How it's made (min 280 chars)

collab is a single Go 1.25+ binary built on five layers stacked inside one peer.

1. **MCP server (internal/mcp).** Built on github.com/mark3labs/mcp-go. Exposes 5 tools the launched agent can call: get_shared_log (inherit context on session join), post_to_log (broadcast a message), ask_peer (direct a question at one peer's agent), respond_to_peer (answer one), set_status (update a "what I'm doing" line), and list_shared_files. The MCP server is registered at user-scope via `claude mcp add` so the agent picks it up without an approval prompt every launch, and unregistered on session exit.

2. **Session state store (internal/store).** In-memory CRDT bundle: a G-Set for the conversation log (keyed by ULID for sortable, dedupable IDs) and a per-file LWW-Register for the shared directory contents (with add-wins-on-delete semantics and tombstones, capped at 256 KB per file).

3. **Sync engine (internal/sync).** fsnotify watcher for ./shared/, echo suppression so a peer's own writes do not bounce back, and replay-on-join so a late joiner inherits the full backlog from the host.

4. **Transport (internal/transport).** Supervises a Gensyn Axl daemon as a child process and speaks to it over localhost HTTP. Axl is the P2P substrate (Yggdrasil mesh + TLS); collab never touches its routing tables. Invites are short tokens carrying the host peer ID and a base64url-encoded public address so a joiner on a different machine knows what to dial.

5. **TUI (cmd/collab).** Built on charmbracelet/bubbletea + lipgloss. Live peer list with statuses, log feed with ask/answer tags, shared-file list, and a footer of keybinds (`[a]` launch claude, `[c]` copy invite, `[q]` quit).

Other technical specifics worth flagging:

- The Axl `node` daemon is auto-built from upstream gensyn-ai/axl into ~/.collab/bin/axl-node on first `collab create` or `collab join`. Users only chase one binary (the install flow is a single curl + sha256 verification).
- A per-session CLAUDE.md is generated at session start, telling the launched Claude Code agent how + when to use the 5 MCP tools (and not to spam the log). This is the bridge between "a generic AI coding agent" and "an agent that knows it's pair-programming."
- No persistence by design (v1). Sessions live in memory and die with the host process. An export verb dumps the log to JSON if the user wants to keep it.
- The transport layer is intentionally replaceable behind a Go interface, so the same store + sync engine could ride on something other than Axl in v2.

Stack: Go 1.25, mark3labs/mcp-go, fsnotify, spf13/cobra, charmbracelet/bubbletea + lipgloss, oklog/ulid, Gensyn Axl, goreleaser for cross-arch release builds (darwin / linux × amd64 / arm64).
