package store

import (
	"sync"
	"testing"
	"time"

	"github.com/Aman035/colabAI/pkg/protocol"
)

func entry(id, peer string, ts time.Time, content string) protocol.LogEntry {
	return protocol.LogEntry{
		ID:        id,
		Timestamp: ts,
		PeerID:    peer,
		Role:      protocol.RoleUser,
		Content:   content,
	}
}

func TestAppendAndOrder(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)

	// Insert out of order; the store sorts by timestamp.
	if err := s.AppendEntry(entry("c", "A", t0.Add(2*time.Second), "third"), SourceLocal); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEntry(entry("a", "A", t0, "first"), SourceLocal); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEntry(entry("b", "A", t0.Add(time.Second), "second"), SourceLocal); err != nil {
		t.Fatal(err)
	}

	// Drain change channel so we don't fill it.
	for range 3 {
		<-s.Subscribe()
	}

	got := s.EntriesSince(time.Time{})
	want := []string{"first", "second", "third"}
	if len(got) != 3 {
		t.Fatalf("len: got %d", len(got))
	}
	for i, w := range want {
		if got[i].Content != w {
			t.Fatalf("pos %d: got %q want %q", i, got[i].Content, w)
		}
	}
}

func TestAppendIsIdempotent(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()
	e := entry("dup", "A", t0, "once")

	for range 3 {
		if err := s.AppendEntry(e, SourceLocal); err != nil {
			t.Fatal(err)
		}
	}
	// Only one Change should be emitted.
	<-s.Subscribe()
	select {
	case extra := <-s.Subscribe():
		t.Fatalf("expected no extra changes; got %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
	if got := s.EntriesSince(time.Time{}); len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
}

func TestEntriesSince(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()
	_ = s.AppendEntry(entry("1", "A", t0, "a"), SourceLocal)
	_ = s.AppendEntry(entry("2", "A", t0.Add(time.Second), "b"), SourceLocal)
	_ = s.AppendEntry(entry("3", "A", t0.Add(2*time.Second), "c"), SourceLocal)
	for range 3 {
		<-s.Subscribe()
	}

	got := s.EntriesSince(t0) // strictly after t0
	if len(got) != 2 || got[0].Content != "b" || got[1].Content != "c" {
		t.Fatalf("got %+v", got)
	}
}

func TestConcurrentAppendsAreSafe(t *testing.T) {
	s := New()
	defer s.Close()
	t0 := time.Now().UTC()

	// Drain channel in background to avoid backpressure.
	done := make(chan struct{})
	go func() {
		ch := s.Subscribe()
		for range ch {
		}
		close(done)
	}()

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 50 {
				id := string(rune('A'+g)) + string(rune('0'+i%10)) + string(rune('0'+(i/10)%10))
				_ = s.AppendEntry(entry(id, "p", t0.Add(time.Duration(i)*time.Millisecond), id), SourceLocal)
			}
		}(g)
	}
	wg.Wait()

	// Verify sortedness.
	es := s.EntriesSince(time.Time{})
	for i := 1; i < len(es); i++ {
		if es[i].Timestamp.Before(es[i-1].Timestamp) {
			t.Fatalf("not sorted at %d", i)
		}
	}
}
