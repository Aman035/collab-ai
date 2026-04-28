package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

// hostListenAddr is the TLS endpoint the host advertises for direct peering.
// M1 demo runs on a single machine; M2/M3 will swap this for invite-embedded
// addresses or public bootstrap peers.
const hostListenAddr = "tls://127.0.0.1:9001"

// recvPollInterval is the sleep between empty /recv polls.
const recvPollInterval = 200 * time.Millisecond

// AxlTransport is the production Transport: an HTTP client against a child
// Axl daemon.
type AxlTransport struct {
	daemon *daemon
	http   *http.Client

	role string // "host" | "joiner"

	mu     sync.Mutex
	peers  map[string]PeerInfo
	token  string // host: the token we issued on Host()
	hostID string // joiner: the host we connected to

	in     chan protocol.WireMessage
	events chan PeerEvent
	stop   chan struct{}
}

// NewAxlTransport constructs (but does not start) an Axl-backed Transport.
// Call Host or Join to bring up the daemon.
func NewAxlTransport() *AxlTransport {
	return &AxlTransport{
		http:   &http.Client{Timeout: 30 * time.Second},
		peers:  make(map[string]PeerInfo),
		in:     make(chan protocol.WireMessage, 256),
		events: make(chan PeerEvent, 16),
		stop:   make(chan struct{}),
	}
}

// Host starts an Axl daemon listening on hostListenAddr and mints an invite.
func (a *AxlTransport) Host(ctx context.Context) (Invite, error) {
	d, err := startDaemon(ctx, daemonOpts{
		Listen: []string{hostListenAddr},
		Peers:  nil,
	})
	if err != nil {
		return Invite{}, err
	}
	a.daemon = d
	a.role = "host"

	inv, err := NewInvite(d.peerID)
	if err != nil {
		_ = d.stop()
		return Invite{}, err
	}
	a.token = inv.Token

	go a.recvLoop()
	return inv, nil
}

// Join starts an Axl daemon, peers it with the host's TLS endpoint, then
// completes the application-level handshake (hello / hello_ack).
func (a *AxlTransport) Join(ctx context.Context, invite Invite) error {
	d, err := startDaemon(ctx, daemonOpts{
		Listen: nil,
		Peers:  []string{hostListenAddr},
	})
	if err != nil {
		return err
	}
	a.daemon = d
	a.role = "joiner"
	a.hostID = invite.PeerID

	go a.recvLoop()

	// Send hello to the host.
	now := time.Now().UTC()
	helloBytes, err := protocol.Encode(protocol.KindHello, d.peerID, now, protocol.HelloPayload{
		PeerID:  d.peerID,
		Token:   invite.Token,
		Version: protocol.Version,
	})
	if err != nil {
		_ = d.stop()
		return fmt.Errorf("encode hello: %w", err)
	}
	if err := a.sendBytesTo(invite.PeerID, helloBytes); err != nil {
		_ = d.stop()
		return fmt.Errorf("send hello: %w", err)
	}

	// Wait for hello_ack on the events channel. The recvLoop populates
	// events after handling control messages.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("host did not respond to hello within 5s")
		case ev := <-a.events:
			// Re-publish to consumer; we've already observed it.
			go func(ev PeerEvent) { a.events <- ev }(ev)
			if ev.Kind == PeerJoined && ev.Peer.ID == invite.PeerID {
				return nil
			}
		}
	}
}

// Send broadcasts to every joined peer.
func (a *AxlTransport) Send(msg protocol.WireMessage) error {
	bytes, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	a.mu.Lock()
	targets := make([]string, 0, len(a.peers))
	for id := range a.peers {
		targets = append(targets, id)
	}
	a.mu.Unlock()
	for _, id := range targets {
		if err := a.sendBytesTo(id, bytes); err != nil {
			slog.Warn("axl: send failed", "peer", id, "err", err)
		}
	}
	return nil
}

func (a *AxlTransport) sendBytesTo(peerID string, body []byte) error {
	req, err := http.NewRequest("POST", a.daemon.apiURL+"/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Destination-Peer-Id", peerID)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("axl send: %s: %s", resp.Status, body)
	}
	return nil
}

// Receive returns the channel of incoming application-level messages.
// Control messages (hello/hello_ack/goodbye) are handled internally and do
// not appear here.
func (a *AxlTransport) Receive() <-chan protocol.WireMessage { return a.in }

// Peers returns the currently joined peers.
func (a *AxlTransport) Peers() []PeerInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]PeerInfo, 0, len(a.peers))
	for _, p := range a.peers {
		out = append(out, p)
	}
	return out
}

// Events returns peer join/leave events.
func (a *AxlTransport) Events() <-chan PeerEvent { return a.events }

// PeerID is our own peer ID, derived from /topology at startup.
func (a *AxlTransport) PeerID() string {
	if a.daemon == nil {
		return ""
	}
	return a.daemon.peerID
}

// Close shuts down the recv loop and the daemon.
func (a *AxlTransport) Close() error {
	select {
	case <-a.stop:
	default:
		close(a.stop)
	}
	// Best-effort goodbye to all peers, with a tight timeout. We don't
	// care if the peer is already gone — daemon teardown is the
	// authoritative shutdown.
	if a.daemon != nil {
		fastClient := &http.Client{Timeout: 500 * time.Millisecond}
		now := time.Now().UTC()
		for _, p := range a.Peers() {
			body, err := protocol.Encode(protocol.KindGoodbye, a.PeerID(), now, protocol.GoodbyePayload{Reason: "shutdown"})
			if err != nil {
				continue
			}
			req, err := http.NewRequest("POST", a.daemon.apiURL+"/send", bytes.NewReader(body))
			if err != nil {
				continue
			}
			req.Header.Set("X-Destination-Peer-Id", p.ID)
			req.Header.Set("Content-Type", "application/octet-stream")
			if resp, err := fastClient.Do(req); err == nil {
				resp.Body.Close()
			}
		}
		return a.daemon.stop()
	}
	return nil
}

// recvLoop long-polls /recv. Control messages are dispatched internally;
// log_entry and file_chunk are forwarded to the public Receive channel.
func (a *AxlTransport) recvLoop() {
	for {
		select {
		case <-a.stop:
			return
		default:
		}
		resp, err := a.http.Get(a.daemon.apiURL + "/recv")
		if err != nil {
			slog.Warn("axl: recv failed", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if resp.StatusCode == http.StatusNoContent {
			resp.Body.Close()
			time.Sleep(recvPollInterval)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			slog.Warn("axl: recv non-200", "status", resp.Status, "body", string(body))
			time.Sleep(time.Second)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			slog.Warn("axl: recv read", "err", err)
			continue
		}
		var msg protocol.WireMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			slog.Warn("axl: recv decode", "err", err, "body_len", len(body))
			continue
		}
		a.dispatch(msg)
	}
}

// dispatch routes one inbound message: control kinds update the peer table;
// data kinds are forwarded to the application.
func (a *AxlTransport) dispatch(msg protocol.WireMessage) {
	switch msg.Kind {
	case protocol.KindHello:
		a.handleHello(msg)
	case protocol.KindHelloAck:
		a.handleHelloAck(msg)
	case protocol.KindGoodbye:
		a.handleGoodbye(msg)
	case protocol.KindLogEntry, protocol.KindFileChunk:
		select {
		case a.in <- msg:
		default:
			slog.Warn("axl: receive channel full, dropping oldest")
			select {
			case <-a.in:
			default:
			}
			select {
			case a.in <- msg:
			default:
			}
		}
	default:
		slog.Warn("axl: unknown kind", "kind", msg.Kind)
	}
}

func (a *AxlTransport) handleHello(msg protocol.WireMessage) {
	var p protocol.HelloPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		slog.Warn("axl: hello malformed", "err", err)
		return
	}

	// Joiners shouldn't receive hello (they only initiated). Hosts validate.
	if a.role != "host" {
		return
	}
	now := time.Now().UTC()
	accepted := p.Token == a.token && p.Version != "" // basic check
	ackPayload := protocol.HelloAckPayload{
		Accepted:   accepted,
		HostPeerID: a.PeerID(),
	}
	if !accepted {
		ackPayload.Error = "invite is invalid or has expired"
	}
	ack, err := protocol.Encode(protocol.KindHelloAck, a.PeerID(), now, ackPayload)
	if err == nil {
		_ = a.sendBytesTo(p.PeerID, ack)
	}
	if accepted {
		a.addPeer(PeerInfo{ID: p.PeerID})
	}
}

func (a *AxlTransport) handleHelloAck(msg protocol.WireMessage) {
	var p protocol.HelloAckPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		slog.Warn("axl: hello_ack malformed", "err", err)
		return
	}
	if !p.Accepted {
		slog.Error("axl: host rejected hello", "err", p.Error)
		return
	}
	a.addPeer(PeerInfo{ID: p.HostPeerID})
}

func (a *AxlTransport) handleGoodbye(msg protocol.WireMessage) {
	a.removePeer(msg.PeerID)
}

func (a *AxlTransport) addPeer(p PeerInfo) {
	a.mu.Lock()
	if _, exists := a.peers[p.ID]; exists {
		a.mu.Unlock()
		return
	}
	a.peers[p.ID] = p
	a.mu.Unlock()
	select {
	case a.events <- PeerEvent{Kind: PeerJoined, Peer: p}:
	default:
	}
}

func (a *AxlTransport) removePeer(id string) {
	a.mu.Lock()
	p, exists := a.peers[id]
	delete(a.peers, id)
	a.mu.Unlock()
	if exists {
		select {
		case a.events <- PeerEvent{Kind: PeerLeft, Peer: p}:
		default:
		}
	}
}
