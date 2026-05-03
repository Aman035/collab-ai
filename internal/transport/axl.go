package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

// defaultListenAddr is the host's Axl TLS bind address when --listen is
// not set. 0.0.0.0 lets joiners on other machines connect; pin to
// 127.0.0.1 if you only want loopback peering.
const defaultListenAddr = "tls://0.0.0.0:9001"

// recvPollInterval is the sleep between empty /recv polls.
const recvPollInterval = 200 * time.Millisecond

// AxlTransport is the production Transport: an HTTP client against a child
// Axl daemon.
type AxlTransport struct {
	daemon *daemon
	http   *http.Client

	role       string // "host" | "joiner"
	myName     string // our friendly handle, included in hello/hello_ack
	sessionID  string // host-assigned identifier for this session
	listenAddr string // host: TLS bind address (defaultListenAddr if unset)
	publicAddr string // host: TLS endpoint embedded in invite (defaults to listenAddr)

	mu       sync.Mutex
	peers    map[string]PeerInfo
	token    string // host: the token we issued on Host()
	hostID   string // joiner: the host we connected to
	myStatus string // last status we broadcast

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

// SetIdentity configures our friendly handle and (host-only) session ID.
// Call before Host or Join. Both fields are optional — peers without a
// handle are displayed by their hex peer ID.
func (a *AxlTransport) SetIdentity(name, sessionID string) {
	a.myName = name
	a.sessionID = sessionID
}

// SetNetwork configures host-only network endpoints before Host():
//
//   - listenAddr is what the local Axl daemon binds to
//     (e.g. "tls://0.0.0.0:9001"). Empty = defaultListenAddr.
//   - publicAddr is what gets embedded in the invite for joiners to dial
//     (e.g. "tls://server1.example.com:9001"). Empty = use listenAddr.
//     For a single-machine demo, both can be left empty.
func (a *AxlTransport) SetNetwork(listenAddr, publicAddr string) {
	a.listenAddr = listenAddr
	a.publicAddr = publicAddr
}

// Host starts an Axl daemon listening on the configured listen address
// (SetNetwork; defaults to defaultListenAddr) and mints an invite. The
// invite embeds the publicAddr so joiners on other machines know what
// endpoint to dial.
func (a *AxlTransport) Host(ctx context.Context) (Invite, error) {
	listen := a.listenAddr
	if listen == "" {
		listen = defaultListenAddr
	}
	d, err := startDaemon(ctx, daemonOpts{
		Listen: []string{listen},
		Peers:  nil,
	})
	if err != nil {
		return Invite{}, err
	}
	a.daemon = d
	a.role = "host"

	publicAddr := a.publicAddr
	if publicAddr == "" {
		publicAddr = listen
	}
	inv, err := NewInvite(d.peerID, publicAddr)
	if err != nil {
		_ = d.stop()
		return Invite{}, err
	}
	a.token = inv.Token

	go a.recvLoop()
	return inv, nil
}

// Join starts an Axl daemon, peers it with the host's TLS endpoint, and
// fires off a hello to the host. Returns as soon as the daemon is ready
// and the hello is on the wire — the hello_ack arrives later via recvLoop
// and triggers a PeerJoined event for the consumer.
//
// Returning fast matters: when collab-ai is spawned as an MCP server
// child, the parent (e.g. Claude Code) has a startup timeout for the MCP
// initialize handshake. Blocking here on a 5s ack window can exceed that
// budget. A failed handshake surfaces as a missing PeerJoined event
// rather than an error from this call.
func (a *AxlTransport) Join(ctx context.Context, invite Invite) error {
	peerAddr := invite.Addr
	if peerAddr == "" {
		peerAddr = DefaultPeerAddr
	}
	d, err := startDaemon(ctx, daemonOpts{
		Listen: nil,
		Peers:  []string{peerAddr},
	})
	if err != nil {
		return err
	}
	a.daemon = d
	a.role = "joiner"
	a.hostID = invite.PeerID

	go a.recvLoop()

	now := time.Now().UTC()
	helloBytes, err := protocol.Encode(protocol.KindHello, d.peerID, now, protocol.HelloPayload{
		PeerID:  d.peerID,
		Token:   invite.Token,
		Version: protocol.Version,
		Name:    a.myName,
	})
	if err != nil {
		_ = d.stop()
		return fmt.Errorf("encode hello: %w", err)
	}
	if err := a.sendBytesTo(invite.PeerID, helloBytes); err != nil {
		_ = d.stop()
		return fmt.Errorf("send hello: %w", err)
	}
	return nil
}

// SendTo delivers msg to a single peer.
func (a *AxlTransport) SendTo(peerID string, msg protocol.WireMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return a.sendBytesTo(peerID, body)
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

// BroadcastStatus updates our status line and sends it to every joined peer.
// Implemented as a peer_status WireMessage; receivers update their peer
// table on intake.
func (a *AxlTransport) BroadcastStatus(status string) error {
	a.mu.Lock()
	a.myStatus = status
	a.mu.Unlock()
	body, err := protocol.Encode(
		protocol.KindPeerStatus, a.PeerID(), time.Now().UTC(),
		protocol.PeerStatusPayload{Status: status},
	)
	if err != nil {
		return err
	}
	for _, p := range a.Peers() {
		if err := a.sendBytesTo(p.ID, body); err != nil {
			slog.Warn("axl: peer_status send failed", "peer", p.ID, "err", err)
		}
	}
	return nil
}

// MyStatus returns our last broadcast status (empty if never set).
func (a *AxlTransport) MyStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.myStatus
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

// Name returns our friendly handle, set via SetIdentity.
func (a *AxlTransport) Name() string { return a.myName }

// SessionID returns the session identifier — host's own value, or the
// host's value adopted from hello_ack (joiner side).
func (a *AxlTransport) SessionID() string { return a.sessionID }

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
	case protocol.KindPeerStatus:
		a.handlePeerStatus(msg)
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
	accepted := p.Token == a.token && p.Version != ""
	ackPayload := protocol.HelloAckPayload{
		Accepted:   accepted,
		HostPeerID: a.PeerID(),
		HostName:   a.myName,
		SessionID:  a.sessionID,
	}
	if !accepted {
		ackPayload.Error = "invite is invalid or has expired"
	}
	ack, err := protocol.Encode(protocol.KindHelloAck, a.PeerID(), now, ackPayload)
	if err == nil {
		_ = a.sendBytesTo(p.PeerID, ack)
	}
	if accepted {
		a.addPeer(PeerInfo{ID: p.PeerID, Name: p.Name})
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
	if a.sessionID == "" {
		a.sessionID = p.SessionID // joiner adopts the host's session ID
	}
	a.addPeer(PeerInfo{ID: p.HostPeerID, Name: p.HostName})
}

func (a *AxlTransport) handleGoodbye(msg protocol.WireMessage) {
	a.removePeer(msg.PeerID)
}

func (a *AxlTransport) handlePeerStatus(msg protocol.WireMessage) {
	var p protocol.PeerStatusPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		slog.Warn("axl: peer_status malformed", "err", err)
		return
	}
	a.mu.Lock()
	if existing, ok := a.peers[msg.PeerID]; ok {
		existing.Status = p.Status
		a.peers[msg.PeerID] = existing
	}
	a.mu.Unlock()
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
