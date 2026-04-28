// Package protocol defines the wire types exchanged between collab-ai peers.
//
// This package is intentionally public so a peer in another language can
// implement a compatible peer by reading these types.
package protocol

import (
	"encoding/json"
	"time"
)

// Version is the protocol version. Major-version mismatches are rejected at
// handshake. See docs/07-protocol.md.
const Version = "0.1.0"

// Wire envelope kinds.
const (
	KindHello     = "hello"
	KindHelloAck  = "hello_ack"
	KindGoodbye   = "goodbye"
	KindLogEntry  = "log_entry"
	KindFileChunk = "file_chunk"
)

// Roles for log entries.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// MaxFileBytes caps `file_chunk` payload Content size (256 KB).
const MaxFileBytes = 256 * 1024

// WireMessage is the top-level envelope sent over the transport.
//
// PeerID is the authoritative originator. The transport's view of the sender
// (Axl's X-From-Peer-Id) is lossy; never trust it. Echo suppression and
// identity comparisons MUST read this field.
type WireMessage struct {
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	PeerID    string          `json:"peer_id"`
	Timestamp time.Time       `json:"timestamp"`
}

// LogEntry is a single conversation entry. Doubles as the wire payload for
// `log_entry` messages and the in-memory representation in the Store.
type LogEntry struct {
	ID        string    `json:"id"`        // ULID
	Timestamp time.Time `json:"timestamp"` // RFC3339
	PeerID    string    `json:"peer_id"`   // originator
	Role      string    `json:"role"`      // "user" | "assistant"
	Content   string    `json:"content"`
}

// FileMeta indexes a file in ./shared/. Doubles as the in-memory file index
// entry; PeerID + ModTime form the LWW-Register version for CRDT merge.
type FileMeta struct {
	Path    string    `json:"path"`     // relative to ./shared/
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modified"`
	Hash    string    `json:"hash"` // sha256 hex
	PeerID  string    `json:"peer_id"`
}

// HelloPayload — joiner identifies itself + presents invite token.
type HelloPayload struct {
	PeerID  string `json:"peer_id"`
	Token   string `json:"token,omitempty"` // present only on join → host
	Version string `json:"version"`
}

// HelloAckPayload — host accepts or rejects.
type HelloAckPayload struct {
	Accepted   bool   `json:"accepted"`
	Error      string `json:"error,omitempty"`
	HostPeerID string `json:"host_peer_id"`
}

// GoodbyePayload — sent before clean disconnect.
type GoodbyePayload struct {
	Reason string `json:"reason,omitempty"`
}

// FileChunkPayload — a file added, changed, or deleted.
type FileChunkPayload struct {
	Path    string `json:"path"`
	Content []byte `json:"content,omitempty"` // base64 when JSON-encoded; empty if Deleted
	Hash    string `json:"hash,omitempty"`    // sha256 hex of Content (empty when Deleted)
	Deleted bool   `json:"deleted"`
}

// Encode marshals payload, wraps it in a WireMessage with the given envelope
// fields, and returns the JSON bytes. Helper used by the transport.
func Encode(kind, peerID string, ts time.Time, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(WireMessage{
		Kind:      kind,
		Payload:   raw,
		PeerID:    peerID,
		Timestamp: ts,
	})
}
