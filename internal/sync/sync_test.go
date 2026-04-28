package sync

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Aman035/collab-ai/internal/store"
	"github.com/Aman035/collab-ai/internal/transport"
	"github.com/Aman035/collab-ai/pkg/protocol"
)

// Two engines wired through a MemHub: an entry written on A appears on B.
func TestLogEntrySyncsBetweenPeers(t *testing.T) {
	hub := transport.NewMemHub()
	tA := hub.New("peer-A")
	tB := hub.New("peer-B")

	sA := store.New()
	sB := store.New()
	defer sA.Close()
	defer sB.Close()

	eA := New(sA, tA, "peer-A", "")
	eB := New(sB, tB, "peer-B", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eA.Run(ctx)
	go eB.Run(ctx)

	now := time.Now().UTC()
	if err := sA.AppendEntry(protocol.LogEntry{
		ID:        "01",
		Timestamp: now,
		PeerID:    "peer-A",
		Role:      protocol.RoleUser,
		Content:   "hello from A",
	}, store.SourceLocal); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sB.EntriesSince(time.Time{}); len(got) == 1 && got[0].Content == "hello from A" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("entry never reached B: %+v", sB.EntriesSince(time.Time{}))
}

// A's own broadcast must not loop back into A's store.
func TestEchoSuppression(t *testing.T) {
	hub := transport.NewMemHub()
	tA := hub.New("peer-A")
	// Single transport: A is the only peer. Send-to-self via hub still
	// shouldn't reappear because hub.Send skips the sender. To exercise
	// echo suppression at the engine, inject a message directly with our
	// own PeerID and verify it isn't applied.

	sA := store.New()
	defer sA.Close()
	eA := New(sA, tA, "peer-A", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eA.Run(ctx)

	// Build a fake inbound message that *claims* to be from peer-A.
	now := time.Now().UTC()
	raw, _ := protocol.Encode(protocol.KindLogEntry, "peer-A", now, protocol.LogEntry{
		ID: "echo", Timestamp: now, PeerID: "peer-A", Role: protocol.RoleUser, Content: "echo",
	})

	// We need to push this to tA's recv channel. MemTransport doesn't
	// expose that directly; simulate by routing through a sibling.
	tB := hub.New("peer-B")
	defer tB.Close()
	var msg protocol.WireMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	if err := tB.Send(msg); err != nil {
		t.Fatal(err)
	}

	// Give the engine a chance to drop it.
	time.Sleep(50 * time.Millisecond)
	if got := sA.EntriesSince(time.Time{}); len(got) != 0 {
		t.Fatalf("echo applied: %+v", got)
	}
}

