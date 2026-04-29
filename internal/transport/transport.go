// Package transport moves WireMessages between peers. The Transport
// interface is consumer-defined here; concrete implementations are the Axl
// HTTP client (see axl.go) and the in-memory stub (memory.go) used in tests.
//
// No Axl-specific types cross this boundary. The rest of the project sees
// only this interface and pkg/protocol types.
package transport

import (
	"context"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

// Invite is a host's invite for joiners. The Code is human-readable; PeerID
// and Token are extracted from it on Join.
type Invite struct {
	Code   string
	PeerID string
	Token  string
}

// PeerInfo is what we know about a connected peer.
type PeerInfo struct {
	ID   string
	Name string // friendly handle if the peer announced one (e.g. "clever-otter")
	Addr string // for display only — Axl provides nothing here, may be empty
}

// PeerEvent is fired on join/leave at the application level (after our
// hello/hello_ack handshake). Axl itself has no concept of an application
// session, so we infer these events from our own traffic.
type PeerEvent struct {
	Kind string // "joined" | "left"
	Peer PeerInfo
}

const (
	PeerJoined = "joined"
	PeerLeft   = "left"
)

// Transport is the byte-level send/receive surface used by internal/sync.
type Transport interface {
	// Host starts listening, returns an invite for joiners.
	Host(ctx context.Context) (Invite, error)

	// Join connects to the host described by the invite.
	Join(ctx context.Context, invite Invite) error

	// Send broadcasts a WireMessage to all currently joined peers
	// (best-effort; logs and continues on per-peer failures).
	Send(msg protocol.WireMessage) error

	// SendTo delivers a WireMessage to a specific peer. Used by the Sync
	// Engine to replay existing state to a freshly-joined peer.
	SendTo(peerID string, msg protocol.WireMessage) error

	// Receive returns inbound WireMessages. Control messages
	// (hello/hello_ack/goodbye) are handled by the transport itself and
	// are not surfaced on this channel.
	Receive() <-chan protocol.WireMessage

	// Peers returns the currently joined peers.
	Peers() []PeerInfo

	// Events returns peer join/leave events.
	Events() <-chan PeerEvent

	// PeerID is this node's peer ID.
	PeerID() string

	// Close shuts down all connections and any owned subprocesses.
	Close() error
}
