package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withFakeHome redirects HOME so Path() resolves into a temp dir.
func withFakeHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
}

func TestWriteRoundtrip(t *testing.T) {
	withFakeHome(t)

	in := &File{
		FileVersion: FileVersion,
		Version:     "0.1.0",
		SessionID:   "sess-1",
		StartedAt:   time.Now().UTC().Truncate(time.Second),
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
		Role:        "host",
		InviteCode:  "COLLAB-aaa-bbb",
		PeerID:      "peer-A",
		SharedDir:   "./shared",
		Peers:       []Peer{{ID: "peer-A", JoinedAt: time.Now().UTC().Truncate(time.Second), Self: true}},
	}
	if err := Write(in); err != nil {
		t.Fatal(err)
	}

	got, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != in.SessionID || got.InviteCode != in.InviteCode || len(got.Peers) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}

	p, _ := Path()
	if !filepath.IsAbs(p) {
		t.Fatalf("path should be absolute, got %q", p)
	}
}

func TestRemove(t *testing.T) {
	withFakeHome(t)
	_ = Write(&File{SessionID: "x"})
	if err := Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(); !os.IsNotExist(err) {
		t.Fatalf("expected IsNotExist after Remove, got %v", err)
	}
	// Idempotent: removing again is fine.
	if err := Remove(); err != nil {
		t.Fatal(err)
	}
}
