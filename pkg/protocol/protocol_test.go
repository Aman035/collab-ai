package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWireMessageRoundtrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	entry := LogEntry{
		ID:        "01H8XYZ",
		Timestamp: now,
		PeerID:    "peer-A",
		Role:      RoleUser,
		Content:   "hello",
	}
	bytes, err := Encode(KindLogEntry, "peer-A", now, entry)
	if err != nil {
		t.Fatal(err)
	}

	var got WireMessage
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindLogEntry || got.PeerID != "peer-A" || !got.Timestamp.Equal(now) {
		t.Fatalf("envelope: %+v", got)
	}

	var gotEntry LogEntry
	if err := json.Unmarshal(got.Payload, &gotEntry); err != nil {
		t.Fatal(err)
	}
	if gotEntry != entry {
		t.Fatalf("entry mismatch: got %+v, want %+v", gotEntry, entry)
	}
}

func TestUnknownKindIsIgnorable(t *testing.T) {
	// Forward compatibility: an unknown Kind must not break decoding.
	raw := []byte(`{"kind":"some_future_kind","payload":{},"peer_id":"X","timestamp":"2026-01-01T00:00:00Z"}`)
	var msg WireMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unknown kind should decode: %v", err)
	}
	if msg.Kind != "some_future_kind" {
		t.Fatalf("kind: got %q", msg.Kind)
	}
}
