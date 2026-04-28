# 06 · Component: Axl Transport

**Package:** `internal/transport`
**Build estimate:** ~500 LOC (was 400; +100 for daemon supervision)
**Depends on:** the Axl `node` binary at runtime (HTTP API client, no Go SDK), `os/exec`, `net/http`, `pkg/protocol`

> ✅ **De-risked in M0.** Axl has no Go SDK — it's a daemon (`./node`) exposing an HTTP API on `localhost:9002`. Three endpoints we care about: `GET /topology`, `POST /send` (`X-Destination-Peer-Id` header), `GET /recv` (returns body + lossy `X-From-Peer-Id` header). Peering between daemons happens via TLS; routing happens through the Yggdrasil mesh once two daemons share at least one common peer. See `experiments/axl-hello/README.md` for the verified setup.

## Purpose

Move WireMessages between peers. Owns the Axl daemon child process, peer discovery (via invite codes), and the byte-level HTTP send/receive against the daemon.

## Responsibilities

- **Supervise the Axl daemon** as a child process: write a config to a per-session temp dir, `os/exec` the `node` binary, watch its stderr/stdout, restart on crash, kill on shutdown. Allocate a free `api_port` per session so multiple collab-ai processes on one machine don't collide.
- Generate an invite code on `Host()`: host's Axl pubkey + short authorization token, encoded into a human-friendly string.
- Accept inbound `hello` messages matching a valid token (token-check is collab-ai-level, not Axl-level — Axl will deliver any message to us).
- Broadcast outbound WireMessages to all currently joined peers via `POST /send`.
- Long-poll `GET /recv` and deliver inbound WireMessages to the Sync Engine via a channel.
- Surface peer join/leave events to the rest of the app (derived from our own `hello` / `goodbye` traffic, not from Axl — Axl has no concept of an application-level session).

## Non-goals

- Does **not** encrypt at the application layer in v1. Axl gives us TLS over the peering links and Yggdrasil's end-to-end mesh encryption; that's enough.
- Does **not** retry or re-deliver lost messages. Best-effort. Higher layers can detect divergence on rejoin.
- Does **not** do public peer discovery. The Axl mesh handles routing; collab-ai's invite codes carry the host's pubkey.
- Does **not** ship the Axl daemon binary itself. We `exec` whatever `node` is on PATH (or at a configured path) and surface a clear install error if missing.

## Public interface

```go
package transport

import (
  "context"
  "github.com/Aman035/colabAI/pkg/protocol"
)

type Invite struct {
  Code   string    // human-readable, e.g. "COLLAB-7K2P-9XLM"
  PeerID string
  Token  string    // ephemeral; embedded in Code
}

type PeerInfo struct {
  ID   string
  Addr string    // for display in `collab-ai status`
}

type PeerEvent struct {
  Kind   string    // "joined" | "left"
  Peer   PeerInfo
}

type Transport interface {
  // Host starts listening, returns an invite for joiners.
  Host(ctx context.Context) (Invite, error)

  // Join connects to the host described by the invite.
  Join(ctx context.Context, invite Invite) error

  // Send broadcasts a WireMessage to all currently connected peers (best-effort).
  Send(msg protocol.WireMessage) error

  // Receive returns inbound WireMessages.
  Receive() <-chan protocol.WireMessage

  // Peers returns currently connected peers.
  Peers() []PeerInfo

  // Events returns peer join/leave events.
  Events() <-chan PeerEvent

  // PeerID is this node's peer ID.
  PeerID() string

  // Close shuts down all connections.
  Close() error
}
```

## Invite code format

Human-friendly. Generated with sufficient entropy that a guessing attack is not realistic.

Format: `COLLAB-XXXX-XXXX` where each `X` is a Crockford base32 character. 8 chars × 5 bits = 40 bits of token entropy. Plus the peer ID is needed to actually connect.

Encode (peer_id, token) into the suffix; decode on Join. Use a tiny encoding helper — don't bring in a 3rd-party invite library.

## Behavior

### Daemon supervision (shared by `Host` and `Join`)

Both verbs first bring up an Axl daemon as a child process:

1. Pick a free TCP port on `127.0.0.1` for the daemon's `api_port`. Default 9002 if free, otherwise random ephemeral.
2. Write a temp config (`<state_dir>/axl.json`) with: `Listen: []`, `Peers: ["tls://<gensyn-bootstrap-1>:9001", "tls://<gensyn-bootstrap-2>:9001"]`, `api_port: <picked>`, `bridge_addr: "127.0.0.1"`, `tcp_port: 7000`. **`tcp_port` is a protocol constant — see Findings below.**
3. `os/exec` the `node` binary with `-config <state_dir>/axl.json`. Capture stderr; surface the public-key line to the app.
4. Wait until `GET /topology` returns 200; treat that as ready. Timeout: 10s.
5. Read `our_public_key` from `/topology`. That's our `peerID` for the rest of the session.

Shutdown: `goodbye` → close the receive loop → `cmd.Process.Signal(SIGINT)` → wait → kill on timeout.

### `Host`

1. Bring up the daemon (above).
2. Generate a random 40-bit authorization token.
3. Encode `(peerID, token)` into an invite code (`COLLAB-XXXX-XXXX`).
4. Store `token` in memory; the inbound `hello` validator checks it.
5. Start the receive loop (long-poll `GET /recv`).
6. Return the Invite.

### `Join`

1. Decode the invite code → `(host_peer_id, token)`.
2. Bring up the daemon (above).
3. `POST /send` a `hello` WireMessage to `host_peer_id` carrying `token` + our own peerID.
4. Wait for a `hello_ack` on the receive loop. On `accepted: false`, return the error to the CLI; on timeout (5s), return a friendly "host not reachable" error.
5. Cache the host as a known peer in our peer table.

### `Send`

1. JSON-marshal the WireMessage.
2. For each peer in the peer table, `POST /send` with header `X-Destination-Peer-Id: <peer.ID>` and the marshaled bytes as body.
3. Return nil even if some sends fail; log the failures with peer ID + status code. **Do not** block on slow peers — fan out with a small worker pool.

### `Receive`

Returns a buffered channel (capacity ~256). One goroutine long-polls `GET /recv`:

```
for ctx not cancelled:
    resp, err := GET /recv
    if 204 No Content:  short sleep (200ms); continue
    if err:             log; backoff up to 2s; continue
    msg := json.Unmarshal(resp.Body)
    if channel full:    drop oldest; log warn
    else:               push msg
```

The `X-From-Peer-Id` header is **ignored for identity** — see Findings below. The authoritative originator is `msg.PeerID` from inside our envelope.

### `Events`

`PeerEvent{Kind: "joined"}` is pushed when we accept a `hello` from a new peer (or successfully complete a `Join`). `PeerEvent{Kind: "left"}` is pushed when we receive a `goodbye`, or when `POST /send` to that peer has failed N times in a row (heuristic timeout — Axl gives us no liveness signal). N=5, with a 30s window.

## Findings from M0 — bake these into the implementation

### `tcp_port` is a protocol constant

Axl's per-daemon `tcp_port` (default 7000) is the gVisor-internal TCP listener port the *dialer* uses to reach the destination. If two peers' daemons disagree on it, `POST /send` returns `502 Bad Gateway: connection refused`. Do not expose this as a CLI flag. Hard-code `7000` in the supervisor's config writer.

### `X-From-Peer-Id` is lossy — never use it for identity

Tested round-trip: Axl reconstructs the from-peer-id from the Yggdrasil IPv6 source, which only encodes ~120 bits of the 256-bit pubkey. The remaining bytes come back as `0x7f` followed by `0xff` padding. Routing on the destination side (`X-Destination-Peer-Id` in `/send`) accepts the **full** key and works correctly; only the receiver-side `X-From-Peer-Id` is truncated.

**Implication:** echo suppression in the Sync Engine and identity comparisons everywhere must use `WireMessage.PeerID` from inside our JSON envelope. We control that field; we set it on `Send`; we trust it on `Receive`. The `X-From-Peer-Id` header is at most a routing-prefix sanity check.

## Failure modes

| Situation | Behavior |
|-----------|----------|
| `node` binary not on PATH | Print: `axl 'node' binary not found — install from https://github.com/gensyn-ai/axl and put it on PATH`. Exit non-zero. |
| Daemon fails to come up within 10s | Capture last 50 lines of daemon stderr, surface as part of the error. |
| Daemon crashes mid-session | Restart once; if it crashes again within 30s, exit the session with a clear error. |
| Bootstrap peers unreachable | Surface "could not join the Axl mesh; check your network" — the daemon's `/topology` will show no peers. Exit on join; warn on host. |
| Invalid / expired invite | Reject in `hello_ack`. Joiner gets `"invite is invalid or has expired"`. |
| Peer crash mid-session | No Axl-level heartbeat. Detect via N consecutive `POST /send` failures (see `Events`). Emit `left`. Session continues. |
| Host crash | Joiners' sends to the host start failing; after N strikes, emit `left`, print a message, exit non-zero. |
| Send to disconnected peer | Log warning, continue. Don't queue and retry. |
| Receive channel full | Drop oldest, log warning. Do not block. |

## Implementation notes

- All Axl-specific surface (HTTP URLs, header names, daemon supervision) stays inside `internal/transport`. The rest of the project sees only the `Transport` interface and `pkg/protocol` types. **No Axl types or HTTP types should leak into `internal/sync` or `pkg/protocol`.**
- Use `net/http`. No third-party HTTP client; the API is small enough.
- One goroutine per concern: (a) daemon supervisor, (b) recv long-poller, (c) send fan-out workers (small pool, ~4). Each owns a context and stops cleanly.
- The recv long-poll uses a single keep-alive HTTP client with a generous read timeout. Don't open a new connection on every poll.
- An in-memory `Transport` stub (`testing.go`) for the rest of the codebase to test against. Echo suppression and round-trip tests in `internal/sync` use the stub, not a real daemon.

## Acceptance criteria

A two-process test where:

1. Process A calls `Host()` and gets an invite.
2. Process B calls `Join(invite)` successfully.
3. A sends a WireMessage; B receives it on its Receive channel within 100ms.
4. B sends a WireMessage; A receives it.
5. B disconnects (process exits); A receives a `PeerEvent{Kind: "left"}`.
6. C tries to Join with an invalid token; gets a clear error.
