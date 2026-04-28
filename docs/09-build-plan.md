# 09 · Build Plan

Build in milestones. Each milestone has an **acceptance gate** — do not start the next milestone until the current gate passes. This prevents the common failure of "everything is 80% done and nothing works."

## Milestone 0 — De-risk Axl ✅ DONE

Built `experiments/axl-hello/` — 98 LOC Go binary that hits Axl's HTTP API. Verified two daemons on one machine peer over TLS, route messages over the Yggdrasil mesh, and round-trip in both directions.

**Three findings absorbed into the spec:**

1. Axl has no Go SDK; it's a child-process daemon with an HTTP API. `internal/transport` is therefore an HTTP client plus an `os/exec` supervisor.
2. Both peers' daemons must use the same `tcp_port` (default 7000). It's a protocol constant; do not surface as a CLI flag.
3. Axl's `X-From-Peer-Id` is lossy (~120 bits preserved). Identity must come from `WireMessage.PeerID` inside our own envelope. Echo suppression and any peer-identity check uses that field, never the HTTP header.

See `experiments/axl-hello/README.md` for the full setup walkthrough.

## Milestone 1 — Shared log, two peers (CORE DEMO) ✅ ENGINE GATE PASSED

The minimum viable demo. Two `collab-ai` processes can share a conversation log over Axl.

**Status (2026-04-28):** all packages built and unit-tested. Integration test `internal/sync.TestM1Gate` runs two real Axl daemons + two collab-ai stacks on one machine and replicates a log entry host → joiner in **202ms** (target was <1s). Run with `PATH="/path/to/axl:$PATH" go test -tags=integration ./...`.

The hands-on Claude-Code-on-two-terminals validation is left for the demo run; the engine layer is verified.

**What to build:**

- `pkg/protocol` — WireMessage, LogEntry types, JSON encoding
- `internal/store` — in-memory log only (no file index yet); Subscribe channel
- `internal/transport` — Axl daemon supervisor (`os/exec` the `node` binary into a per-session temp dir, wait for `/topology` ready) + HTTP client (`POST /send`, long-poll `GET /recv`) behind the `Transport` interface; `Host()`, `Join()`, `Send()`, `Receive()`. Bootstrap peers in the generated config point at Gensyn's public Axl nodes so the mesh forms automatically. Includes an in-memory `Transport` stub for tests.
- `internal/mcp` — only the `get_shared_log` and `post_to_log` tools (skip `list_shared_files`)
- `internal/sync` — simplest version: subscribe to Store changes, broadcast log_entry messages; on receive, append to Store with `source: "peer"`; echo suppression reads `WireMessage.PeerID` (not Axl's `X-From-Peer-Id` header — it's lossy)
- `cmd/collab-ai` — `host` and `join` subcommands; surface a clear error if the `node` binary isn't on PATH

**Acceptance gate:** Two terminals. Each runs Claude Code pointed at its local `collab-ai` MCP server.

1. Terminal A: `collab-ai host`. Get invite code.
2. Terminal B: `collab-ai join <invite>`. Verify connected.
3. In Claude Code A, ask "what's our session log?" → empty.
4. In Claude Code A, type something. Verify A's Claude appends a log entry.
5. In Claude Code B, ask "what's our session log?" → see A's entry within ~1 second.
6. Vice versa.

**Estimated LOC:** ~700.

**If you cut anything, cut here:**

- The MCP `list_shared_files` tool — defer to Milestone 2.
- Persistence (`status` reading state.json) — defer.
- Pretty CLI output — defer.

## Milestone 2 — Shared directory (full v1 scope)

Add file synchronization. Now the demo can show "B's agent edits a file, A sees it."

**What to build:**

- `internal/shareddir` — gitignore-style filtering, file size cap, path validation
- `internal/store` — add file index methods (UpsertFile, DeleteFile, ListFiles, GetFile)
- `internal/sync` — add fsnotify watcher; outbound file_chunk on local writes; inbound file_chunk handling that hands off to the Store's LWW-Register + add-wins merge
- `pkg/protocol` — add FileChunkPayload
- `internal/mcp` — add `list_shared_files` tool
- `cmd/collab-ai` — wire `--dir` flag through

**Acceptance gate:** Continuing from Milestone 1's two-terminal setup:

1. In `./shared/` on A, create a file `hello.txt`. Verify B's `./shared/hello.txt` appears within ~3 seconds.
2. Edit `hello.txt` on A. Verify B's content updates.
3. Delete `hello.txt` on A. Verify B's file is removed.
4. Edit a 1MB file on A. Verify it is NOT synced (warning logged on A).
5. Edit a `.gitignored` file on A. Verify it is NOT synced.
6. Both A and B edit the same file simultaneously. Verify whichever has the newer ModTime wins; the other peer's content matches the winner.

**Estimated LOC:** +600 over Milestone 1 (~1300 total).

## Milestone 3 — Polish for demo day

Everything works. Make it look professional.

**What to build:**

- `collab-ai status` and `collab-ai export` commands (reading `~/.collab-ai/state.json`)
- Pretty CLI output: invite codes in big colored text, peer-joined messages, etc.
- A README with install instructions and a 30-second demo gif
- goreleaser config for GitHub Releases
- `install.sh` script

**Acceptance gate:**

1. Fresh install via `curl -sSL ... | sh` works on macOS.
2. `collab-ai host` output is presentable on stage.
3. `collab-ai status` from a third terminal shows accurate session info.
4. The README includes a clear quick-start.

**Estimated LOC:** +200.

## Milestone 4 — Stretch (post-demo, optional)

Only attempt if Milestones 0–3 are solid:

- Codex CLI integration test (verifies the agent-agnostic claim)
- Cursor MCP server config docs
- A `collab-ai web` command that serves a read-only HTML view of the session for spectators
- Persistence: write the log to disk, replay on host restart

## Cut points (in priority order if running out of time)

If you're running out of time, drop in this order:

1. ~~Stretch (Milestone 4)~~ — never blocking
2. **Polish (Milestone 3)** — everything except the README. The demo can be slightly rough.
3. **File sync (Milestone 2)** — fall back to log-only demo. Story still works: "shared conversation, no shared accounts." Less impressive, still demoable.
4. **Never cut** the MCP server, the Store, or the Axl transport. Those three plus the Sync Engine's log handling are the project. Without them there is nothing.

## Sequencing rules

- **Do not** start a milestone before the previous one's acceptance gate passes.
- **Do** write tests for components as you build them. Don't save them for the end.
- **Do** keep `experiments/` for throwaway exploration. Don't pollute the main codebase with prototypes.
- **Do not** refactor architecture mid-milestone. If you discover a flaw, finish the current milestone with the bad architecture, then do a clean refactor before the next milestone.
