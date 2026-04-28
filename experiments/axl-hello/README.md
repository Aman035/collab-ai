# Milestone 0 · Axl hello-world

Goal: prove that two Go processes on the same machine can exchange a message
via Gensyn Axl. **Status: passes.** Tested 2026-04-28 with Axl `main` at
commit-of-the-day, Go 1.25.5 (auto-downloaded via toolchain).

## What we found

Axl is **not a Go library**. It's a daemon (`./node`) that runs alongside
your application and exposes a small HTTP API on `localhost:9002`. Three
endpoints matter for v1:

| Method | Path | Purpose |
|--------|------|---------|
| GET    | `/topology` | returns `{"our_public_key": "<hex>", "our_ipv6": ..., ...}` |
| POST   | `/send`     | header `X-Destination-Peer-Id: <hex>`; body = bytes to send |
| GET    | `/recv`     | `200` + raw body + header `X-From-Peer-Id`; `204` if queue empty |

Peer IDs are 64-char hex (ed25519 public keys). Peering between daemons
happens via TLS on port 9001 (configurable in `Listen` / `Peers`); the HTTP
API is purely the local-app-to-daemon channel.

This means our `internal/transport` package becomes a **thin HTTP client** —
no Axl Go types ever cross our boundary. Good for spec discipline.

Upstream docs: https://docs.gensyn.ai/tech/agent-exchange-layer/get-started

## Three findings the spec needs to absorb

### 1. Both peers must share the same `tcp_port` (default 7000)

Axl uses gVisor for an in-userspace TCP stack on top of Yggdrasil's
encrypted IPv6 mesh. The "TCP port" is **inside** the gVisor stack, not on
the OS, so two daemons on the same machine using `tcp_port: 7000` does not
conflict. But the *dialer* (`internal/tcp/dial`) uses its own configured
`tcp_port` to reach the destination — meaning sender and receiver must
agree. Treat `tcp_port` as part of the protocol, not a per-process knob.

### 2. `X-From-Peer-Id` is lossy — only ~120 bits are preserved

Tested both directions. Example A→B:

- A's actual pubkey: `f0c7f37f0c413c3d13ea72f15ce254a6e09657d42d5586dffbb0a2bca98c2da8`
- Header B saw:      `f0c7f37f0c413c3d13ea72f15ce27fffffffffffffffffffffffffffffffffff`

The first ~30 hex chars match, then `7f` followed by `f`-padding. This is
because Axl reconstructs the from-peer-id from the Yggdrasil IPv6 source,
which only encodes a key prefix. The destination side (`X-Destination-Peer-Id`
in `/send`) accepts and routes by the **full** key — only the receiver-side
header is lossy.

**Spec impact (`docs/06-component-transport.md` and `docs/07-protocol.md`):**

- Echo suppression and peer identity must use the **envelope** `peer_id` we
  put inside our own JSON `WireMessage`, **never** Axl's `X-From-Peer-Id`.
- Treat `X-From-Peer-Id` as a routing-prefix sanity check at most.
- Our existing `WireMessage.PeerID` field is exactly the right answer; this
  finding just rules out a "we could simplify by using Axl's header"
  optimization that won't work.

### 3. Setup is now three steps, not one

Every collab-ai user runs **two** processes: the Axl daemon and collab-ai
itself. Peering between Axl daemons requires editing `node-config.json` on
each side and (currently) restarting. The "60-second setup" goal in
`docs/01-overview.md` needs revisiting before M1. Two paths:

a) `collab-ai host` spawns the Axl daemon as a child process with a
   generated config, and the invite code carries the host's public key plus
   reachability hints.
b) Document Axl as a prerequisite the user installs once, like Docker.

(a) is friendlier; (b) is faster to ship. Decide before M1.

## Setup

### 1. Build the Axl node binary

```bash
git clone https://github.com/gensyn-ai/axl.git
cd axl
GOTOOLCHAIN=auto go build -o node ./cmd/node/
```

(Go 1.25+ is auto-downloaded if your local Go is older.)

### 2. Two configs, two ports

For local-loopback testing on one machine, use two daemons that both bind
`tcp_port: 7000` (gVisor-internal — no OS conflict) and differ only in
`api_port`. Node A listens for incoming peers; node B has A in its `Peers`.

`a/node-config.json`:
```json
{
  "Listen": ["tls://127.0.0.1:9001"],
  "Peers": [],
  "api_port": 9002,
  "bridge_addr": "127.0.0.1",
  "tcp_port": 7000
}
```

`b/node-config.json`:
```json
{
  "Listen": [],
  "Peers": ["tls://127.0.0.1:9001"],
  "api_port": 9012,
  "bridge_addr": "127.0.0.1",
  "tcp_port": 7000
}
```

Start them in two terminals:

```bash
cd a && /path/to/node -config node-config.json
# (in another terminal)
cd b && /path/to/node -config node-config.json
```

Each prints its public key on startup. Look for a `Connected outbound` /
`Connected inbound` line on the two daemons before sending — that's the TLS
peering link establishing.

### 3. Build and run the experiment

```bash
go build -o axl-hello .
```

Two more terminals:

```bash
# terminal 3 — listener, talks to node A's daemon
./axl-hello -mode listen -axl http://127.0.0.1:9002
# my peer id: f0c7f37f...
# polling http://127.0.0.1:9002/recv ...
```

Copy node A's full peer ID from this output (or from A's daemon log). Then:

```bash
# terminal 4 — sender, talks to node B's daemon
./axl-hello -mode send -axl http://127.0.0.1:9012 -to f0c7f37f... "hello from B"
# sent.
```

Listener prints:

```
from 8bda7400bb24549eb56eee98be0c7fff...: hello from B
```

(The body is faithful; the from-id is the lossy header described in
finding #2.)

## Acceptance gate — passed

- ✅ Two terminals on the same machine.
- ✅ B successfully sends to A; A receives within ~1s.
- ✅ Round-trip works in reverse direction too.
- ✅ Code under 100 LOC (98 lines of Go, stdlib only).

M0 is done. M1 can start.
