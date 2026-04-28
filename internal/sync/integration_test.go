//go:build integration

package sync

import (
	"context"
	"crypto/rand"
	"os/exec"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Aman035/collab-ai/internal/store"
	"github.com/Aman035/collab-ai/internal/transport"
	"github.com/Aman035/collab-ai/pkg/protocol"
)

// TestM1Gate is the full M1 acceptance gate sans MCP: Two AxlTransports +
// Stores + Sync engines run on the same machine. A log entry written into
// host's Store appears in joiner's Store within ~1s.
//
// Run with: go test -tags=integration -timeout=60s ./internal/sync/...
func TestM1Gate(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("axl 'node' binary not on PATH; skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// HOST stack
	hostTx := transport.NewAxlTransport()
	invite, err := hostTx.Host(ctx)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	defer hostTx.Close()
	hostStore := store.New()
	defer hostStore.Close()
	hostEng := New(hostStore, hostTx, hostTx.PeerID())
	go func() { _ = hostEng.Run(ctx) }()

	// JOINER stack
	joinTx := transport.NewAxlTransport()
	if err := joinTx.Join(ctx, invite); err != nil {
		t.Fatalf("join: %v", err)
	}
	defer joinTx.Close()
	joinStore := store.New()
	defer joinStore.Close()
	joinEng := New(joinStore, joinTx, joinTx.PeerID())
	go func() { _ = joinEng.Run(ctx) }()

	// Wait for handshake to be visible on both sides.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(hostTx.Peers()) > 0 && len(joinTx.Peers()) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Host writes a log entry locally.
	now := time.Now().UTC()
	id := ulid.MustNew(ulid.Timestamp(now), rand.Reader).String()
	entry := protocol.LogEntry{
		ID:        id,
		Timestamp: now,
		PeerID:    hostTx.PeerID(),
		Role:      protocol.RoleUser,
		Content:   "hello from the host side",
	}
	if err := hostStore.AppendEntry(entry, store.SourceLocal); err != nil {
		t.Fatal(err)
	}

	// Wait for it to land in joiner's store.
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := joinStore.EntriesSince(time.Time{})
		if len(got) == 1 && got[0].Content == entry.Content {
			t.Logf("M1 gate passed: log entry replicated in %v", time.Since(now))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("entry never reached joiner: %+v", joinStore.EntriesSince(time.Time{}))
}
