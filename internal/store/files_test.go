package store

import (
	"testing"
	"time"

	"github.com/Aman035/collab-ai/pkg/protocol"
)

func meta(path string, ts time.Time, peer string) protocol.FileMeta {
	return protocol.FileMeta{
		Path: path, ModTime: ts, PeerID: peer, Hash: "h", Size: 1,
	}
}

func drainChanges(t *testing.T, s *Store, n int) {
	t.Helper()
	for range n {
		select {
		case <-s.Subscribe():
		case <-time.After(time.Second):
			t.Fatalf("expected %d changes, timed out", n)
		}
	}
}

func TestUpsertNewerWins(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()

	ok, _ := s.UpsertFile(meta("a", t0, "P"), SourceLocal)
	if !ok {
		t.Fatal("first upsert should be accepted")
	}
	ok, _ = s.UpsertFile(meta("a", t0.Add(-time.Second), "P"), SourcePeer)
	if ok {
		t.Fatal("older upsert must lose")
	}
	ok, _ = s.UpsertFile(meta("a", t0.Add(time.Second), "Q"), SourcePeer)
	if !ok {
		t.Fatal("newer upsert must win")
	}
	got, _ := s.GetFile("a")
	if got.PeerID != "Q" {
		t.Fatalf("winner: got %q want Q", got.PeerID)
	}
	drainChanges(t, s, 2) // 2 accepted upserts
}

func TestUpsertPeerIDBreaksTimestampTie(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()

	_, _ = s.UpsertFile(meta("a", t0, "A"), SourceLocal)
	ok, _ := s.UpsertFile(meta("a", t0, "B"), SourcePeer) // same time, larger peer
	if !ok {
		t.Fatal("higher peer ID should win on equal timestamp")
	}
	got, _ := s.GetFile("a")
	if got.PeerID != "B" {
		t.Fatalf("got %q", got.PeerID)
	}
}

func TestDeleteAddWins(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()

	_, _ = s.UpsertFile(meta("a", t0, "A"), SourceLocal)
	// Concurrent edit "wins" — its (ModTime, PeerID) is greater than the delete's.
	_, _ = s.UpsertFile(meta("a", t0.Add(time.Second), "A"), SourcePeer)
	deleted, _ := s.DeleteFile("a", meta("a", t0.Add(500*time.Millisecond), "Z"), SourcePeer)
	if deleted {
		t.Fatal("edit (newer ModTime) should beat delete")
	}
	if _, ok := s.GetFile("a"); !ok {
		t.Fatal("file should still exist")
	}
}

func TestDeleteThenStaleUpsertHonorsTombstone(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()

	_, _ = s.UpsertFile(meta("a", t0, "A"), SourceLocal)
	deleted, _ := s.DeleteFile("a", meta("a", t0.Add(time.Second), "A"), SourceLocal)
	if !deleted {
		t.Fatal("delete should succeed")
	}
	// A stale upsert from before the delete must be rejected.
	ok, _ := s.UpsertFile(meta("a", t0, "A"), SourcePeer)
	if ok {
		t.Fatal("stale upsert should be rejected by tombstone")
	}
	if _, exists := s.GetFile("a"); exists {
		t.Fatal("file should remain deleted")
	}
}

func TestDeleteThenFreshUpsertRevives(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()

	_, _ = s.UpsertFile(meta("a", t0, "A"), SourceLocal)
	_, _ = s.DeleteFile("a", meta("a", t0.Add(time.Second), "A"), SourceLocal)
	// A fresh upsert (newer than the delete) revives the file.
	ok, _ := s.UpsertFile(meta("a", t0.Add(2*time.Second), "B"), SourcePeer)
	if !ok {
		t.Fatal("fresh upsert should revive")
	}
	if got, _ := s.GetFile("a"); got.PeerID != "B" {
		t.Fatalf("revived file: got peer %q", got.PeerID)
	}
}

func TestListFilesPrefix(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()
	_, _ = s.UpsertFile(meta("src/a.go", t0, "A"), SourceLocal)
	_, _ = s.UpsertFile(meta("src/b.go", t0, "A"), SourceLocal)
	_, _ = s.UpsertFile(meta("docs/r.md", t0, "A"), SourceLocal)

	if got := s.ListFiles("src/"); len(got) != 2 {
		t.Fatalf("src/: got %d", len(got))
	}
	if got := s.ListFiles(""); len(got) != 3 {
		t.Fatalf("all: got %d", len(got))
	}
	if got := s.ListFiles("nope/"); len(got) != 0 {
		t.Fatalf("nope/: got %d", len(got))
	}
}
