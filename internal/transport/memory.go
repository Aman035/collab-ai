package transport

import (
	"context"
	"sync"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

// MemHub is an in-process broadcast medium used by tests. Multiple
// MemTransports register against the same hub and see each other's sends.
// Not for production use.
type MemHub struct {
	mu         sync.Mutex
	transports map[string]*MemTransport
}

// NewMemHub returns an empty hub.
func NewMemHub() *MemHub {
	return &MemHub{transports: make(map[string]*MemTransport)}
}

// New attaches a new MemTransport with the given peerID to the hub.
func (h *MemHub) New(peerID string) *MemTransport {
	t := &MemTransport{
		id:     peerID,
		hub:    h,
		in:     make(chan protocol.WireMessage, 256),
		events: make(chan PeerEvent, 16),
	}
	h.mu.Lock()
	h.transports[peerID] = t
	// Existing peers see the newcomer; the newcomer sees existing peers.
	for id, other := range h.transports {
		if id == peerID {
			continue
		}
		t.peers = append(t.peers, PeerInfo{ID: id})
		other.peers = append(other.peers, PeerInfo{ID: peerID})
		other.events <- PeerEvent{Kind: PeerJoined, Peer: PeerInfo{ID: peerID}}
		t.events <- PeerEvent{Kind: PeerJoined, Peer: PeerInfo{ID: id}}
	}
	h.mu.Unlock()
	return t
}

// MemTransport implements Transport using an in-memory broadcast hub.
type MemTransport struct {
	id     string
	hub    *MemHub
	in     chan protocol.WireMessage
	events chan PeerEvent

	mu       sync.Mutex
	peers    []PeerInfo
	myStatus string
}

func (m *MemTransport) Host(ctx context.Context) (Invite, error) {
	return Invite{Code: "MEM-" + m.id, PeerID: m.id, Token: "test"}, nil
}

func (m *MemTransport) Join(ctx context.Context, invite Invite) error {
	return nil // hub already cross-wired peers in New
}

// Send broadcasts to every other transport in the hub. Echo suppression is
// the receiver's responsibility (compare msg.PeerID to your own).
func (m *MemTransport) Send(msg protocol.WireMessage) error {
	m.hub.mu.Lock()
	targets := make([]*MemTransport, 0, len(m.hub.transports))
	for id, t := range m.hub.transports {
		if id == m.id {
			continue
		}
		targets = append(targets, t)
	}
	m.hub.mu.Unlock()
	for _, t := range targets {
		deliver(t, msg)
	}
	return nil
}

// SendTo delivers msg to a single peer. Drops silently if no such peer is
// in the hub — matches Send's best-effort semantics.
func (m *MemTransport) SendTo(peerID string, msg protocol.WireMessage) error {
	m.hub.mu.Lock()
	t, ok := m.hub.transports[peerID]
	m.hub.mu.Unlock()
	if !ok {
		return nil
	}
	deliver(t, msg)
	return nil
}

func deliver(t *MemTransport, msg protocol.WireMessage) {
	select {
	case t.in <- msg:
		return
	default:
	}
	select {
	case <-t.in:
	default:
	}
	select {
	case t.in <- msg:
	default:
	}
}

// BroadcastStatus stores our status and updates every peer's view of us.
// In the in-memory hub, peers see each other directly via Peers(); we
// don't bounce a wire message — this keeps the test stub simple.
func (m *MemTransport) BroadcastStatus(status string) error {
	m.mu.Lock()
	m.myStatus = status
	m.mu.Unlock()
	m.hub.mu.Lock()
	defer m.hub.mu.Unlock()
	for id, t := range m.hub.transports {
		if id == m.id {
			continue
		}
		t.mu.Lock()
		for i, p := range t.peers {
			if p.ID == m.id {
				t.peers[i].Status = status
			}
		}
		t.mu.Unlock()
	}
	return nil
}

func (m *MemTransport) MyStatus() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.myStatus
}

func (m *MemTransport) Receive() <-chan protocol.WireMessage { return m.in }

func (m *MemTransport) Peers() []PeerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PeerInfo, len(m.peers))
	copy(out, m.peers)
	return out
}

func (m *MemTransport) Events() <-chan PeerEvent { return m.events }

func (m *MemTransport) PeerID() string { return m.id }

func (m *MemTransport) Close() error {
	m.hub.mu.Lock()
	delete(m.hub.transports, m.id)
	for _, other := range m.hub.transports {
		other.events <- PeerEvent{Kind: PeerLeft, Peer: PeerInfo{ID: m.id}}
	}
	m.hub.mu.Unlock()
	close(m.in)
	close(m.events)
	return nil
}
