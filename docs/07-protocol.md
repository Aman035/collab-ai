# 07 · Protocol

The format of messages exchanged between peers over the Axl transport. Defined in `pkg/protocol`.

This package is intentionally **public** (`pkg/`, not `internal/`) because anyone wanting to write a compatible peer in another language reads it.

## Encoding

JSON. UTF-8. One message per Axl frame. Fields use snake_case.

(msgpack or protobuf would be smaller; JSON is faster to debug and the message volume is low. Stick with JSON for v1.)

## Top-level message envelope

```go
type WireMessage struct {
  Kind      string          `json:"kind"`        // see below
  Payload   json.RawMessage `json:"payload"`     // decoded based on Kind
  PeerID    string          `json:"peer_id"`     // originator — authoritative identity (see below)
  Timestamp time.Time       `json:"timestamp"`   // when it was sent (RFC3339)
}
```

**Identity rule.** `WireMessage.PeerID` is the **only** authoritative source of the originating peer's identity. The Axl daemon's `X-From-Peer-Id` HTTP header is reconstructed from a Yggdrasil IPv6 address and is lossy (only ~120 bits of the 256-bit pubkey survive; see M0 findings in `experiments/axl-hello/README.md` and `docs/06-component-transport.md`). Echo suppression and any peer-identity comparison in `internal/sync` and elsewhere MUST read `WireMessage.PeerID`, not the HTTP header.

`Kind` is one of:

| Kind | Direction | Purpose |
|------|-----------|---------|
| `hello` | join → host | Joiner identifies itself + presents invite token |
| `hello_ack` | host → join | Host accepts (or rejects in `error`) |
| `goodbye` | any | Sent before clean disconnect |
| `log_entry` | any → all | New conversation log entry |
| `file_chunk` | any → all | File added, changed, or deleted |

## Payload schemas

### `hello`

```go
type HelloPayload struct {
  PeerID  string `json:"peer_id"`
  Token   string `json:"token"`           // from invite, only present on join → host
  Version string `json:"version"`         // semver, e.g. "0.1.0"
}
```

### `hello_ack`

```go
type HelloAckPayload struct {
  Accepted bool   `json:"accepted"`
  Error    string `json:"error,omitempty"`     // populated when Accepted == false
  HostPeerID string `json:"host_peer_id"`
}
```

### `goodbye`

```go
type GoodbyePayload struct {
  Reason string `json:"reason,omitempty"`      // "user_quit" | "shutdown" | ""
}
```

### `log_entry`

```go
type LogEntryPayload struct {
  ID        string    `json:"id"`              // ULID
  Timestamp time.Time `json:"timestamp"`
  PeerID    string    `json:"peer_id"`         // originator (may differ from envelope.PeerID if relayed)
  Role      string    `json:"role"`            // "user" | "assistant"
  Content   string    `json:"content"`
}
```

(Identical shape to `protocol.LogEntry` — the same struct serves both as the wire payload and the in-memory type.)

### `file_chunk`

```go
type FileChunkPayload struct {
  Path    string `json:"path"`                 // relative to ./shared/, never absolute, never contains ".."
  Content []byte `json:"content"`              // base64-encoded JSON; empty if Deleted
  Hash    string `json:"hash"`                 // sha256 hex of Content (empty when Deleted)
  Deleted bool   `json:"deleted"`              // true on delete
}
```

**Constraints:**
- `Path` MUST be relative to `./shared/`, MUST NOT start with `/`, MUST NOT contain `..`.
- `Content` MUST be ≤ 256 KB (262144 bytes) when not Deleted. Larger payloads MUST be rejected by the receiver.
- `Hash` MUST match `sha256(Content)` when not Deleted. Mismatches MUST be logged and the message dropped.

## Handshake sequence

```
Joiner                                       Host
   │                                          │
   │ ────── (TCP/Axl connect) ──────────────▶ │
   │                                          │
   │ ──── hello {peer_id, token, version} ──▶ │
   │                                          │ validates token, version
   │ ◀───  hello_ack {accepted: true, ...} ── │
   │                                          │
   │ ◀──── log_entry, file_chunk, ... ──────▶ │  (steady state, both directions)
   │                                          │
   │ ──────── goodbye ─────────────────────▶  │
   │ ────── (close connection) ─────────────▶ │
```

If `hello_ack.Accepted == false`, the joiner closes the connection and shows `hello_ack.Error` to the user.

## Versioning

The `Version` field in `hello` is a semver string. v1 hosts and joiners MUST be on the same major version. If a joiner connects with an incompatible major, the host MUST respond with `hello_ack{Accepted: false, Error: "version mismatch"}`.

## What does NOT cross the network

For clarity (and so the security model is obvious):

- API keys
- Tool-call results from the agent
- Files outside `./shared/`
- Any environment variables
- Shell command outputs
- The agent's internal state

If you find yourself adding a new payload kind that exposes any of the above, **stop**. The boundary is intentional.

## Forward compatibility

- Unknown `Kind` values MUST be ignored (with a warning log) by receivers, not crashed on.
- Unknown fields in payloads MUST be ignored — use Go's default JSON behavior, which already does this.
- Adding a new payload kind in a minor version is allowed; removing one is a breaking change.
