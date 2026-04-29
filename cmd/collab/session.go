package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Aman035/collab-ai/internal/state"
	"github.com/Aman035/collab-ai/internal/store"
	"github.com/Aman035/collab-ai/internal/transport"
	"github.com/Aman035/collab-ai/pkg/protocol"
)

// session bundles the running info we need for state snapshots and
// peer-event logging. It owns a small peer table keyed by ID so we can
// remember when each peer joined and what handle they announced.
type session struct {
	id         string
	myName     string
	startedAt  time.Time
	role       string
	peerID     string
	sharedDir  string
	inviteCode string

	store *store.Store
	tx    *transport.AxlTransport

	mu    sync.Mutex
	peers map[string]peerEntry // id -> joined_at + handle
}

type peerEntry struct {
	JoinedAt time.Time
	Name     string
}

func newSession(role, peerID, sharedDir, invite string, st *store.Store, tx *transport.AxlTransport) *session {
	return &session{
		id:         randHex(8),
		startedAt:  time.Now().UTC(),
		role:       role,
		peerID:     peerID,
		sharedDir:  sharedDir,
		inviteCode: invite,
		store:      st,
		tx:         tx,
		peers:      make(map[string]peerEntry),
	}
}

// trackPeerEvents keeps the peer table updated and forwards each event to
// the visible logger. Runs until the events channel closes.
func (s *session) trackPeerEvents(log func(transport.PeerEvent)) {
	for ev := range s.tx.Events() {
		s.mu.Lock()
		switch ev.Kind {
		case transport.PeerJoined:
			s.peers[ev.Peer.ID] = peerEntry{JoinedAt: time.Now().UTC(), Name: ev.Peer.Name}
		case transport.PeerLeft:
			delete(s.peers, ev.Peer.ID)
		}
		s.mu.Unlock()
		if log != nil {
			log(ev)
		}
	}
}

// snapshot builds a state.File from the live session. The state writer's
// goroutine calls this every tick.
func (s *session) snapshot() *state.File {
	entries := s.store.EntriesSince(time.Time{})
	files := s.store.ListFiles("")

	s.mu.Lock()
	peers := make([]state.Peer, 0, len(s.peers)+1)
	peers = append(peers, state.Peer{
		ID: s.peerID, Name: s.myName, JoinedAt: s.startedAt, Self: true,
	})
	for id, p := range s.peers {
		peers = append(peers, state.Peer{ID: id, Name: p.Name, JoinedAt: p.JoinedAt})
	}
	s.mu.Unlock()

	return &state.File{
		FileVersion: state.FileVersion,
		Version:     protocol.Version,
		SessionID:   s.id,
		StartedAt:   s.startedAt,
		Role:        s.role,
		InviteCode:  s.inviteCode,
		PeerID:      s.peerID,
		SharedDir:   s.sharedDir,
		Peers:       peers,
		Entries:     entries,
		Files:       files,
	}
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
