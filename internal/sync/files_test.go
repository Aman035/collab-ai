package sync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aman035/collab-ai/internal/store"
	"github.com/Aman035/collab-ai/internal/transport"
)

// twoEngines wires two MemHub-connected engines, each with its own shared
// dir. Returns dirs and a teardown function.
func twoEngines(t *testing.T) (
	dirA, dirB string, sA, sB *store.Store, cancel context.CancelFunc,
) {
	t.Helper()
	dirA = t.TempDir()
	dirB = t.TempDir()
	hub := transport.NewMemHub()
	tA := hub.New("peer-A")
	tB := hub.New("peer-B")
	sA = store.New()
	sB = store.New()
	eA := New(sA, tA, "peer-A", dirA)
	eB := New(sB, tB, "peer-B", dirB)
	ctx, c := context.WithCancel(context.Background())
	go eA.Run(ctx)
	go eB.Run(ctx)
	cancel = func() {
		c()
		sA.Close()
		sB.Close()
		tA.Close()
		tB.Close()
	}
	// Give watchers a moment to register the temp dirs.
	time.Sleep(150 * time.Millisecond)
	return
}

func waitFor(t *testing.T, deadline time.Duration, fn func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", deadline)
}

func TestFileCreateSyncs(t *testing.T) {
	dirA, dirB, _, sB, cancel := twoEngines(t)
	defer cancel()

	if err := os.WriteFile(filepath.Join(dirA, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		b, err := os.ReadFile(filepath.Join(dirB, "hello.txt"))
		return err == nil && string(b) == "hi"
	})
	if _, ok := sB.GetFile("hello.txt"); !ok {
		t.Fatal("file not in B's store")
	}
}

func TestFileEditSyncs(t *testing.T) {
	dirA, dirB, _, _, cancel := twoEngines(t)
	defer cancel()

	p := filepath.Join(dirA, "edit.txt")
	if err := os.WriteFile(p, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		b, err := os.ReadFile(filepath.Join(dirB, "edit.txt"))
		return err == nil && string(b) == "v1"
	})
	// Second write must have a strictly later mod time so the LWW-Register
	// accepts it (we use wall-clock now()).
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(p, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		b, err := os.ReadFile(filepath.Join(dirB, "edit.txt"))
		return err == nil && string(b) == "v2"
	})
}

func TestFileDeleteSyncs(t *testing.T) {
	dirA, dirB, _, sB, cancel := twoEngines(t)
	defer cancel()

	p := filepath.Join(dirA, "doomed.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(dirB, "doomed.txt"))
		return err == nil
	})
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool {
		_, err := os.Stat(filepath.Join(dirB, "doomed.txt"))
		return os.IsNotExist(err)
	})
	if _, ok := sB.GetFile("doomed.txt"); ok {
		t.Fatal("file should be gone from B's store")
	}
}

func TestOversizeFileNotSynced(t *testing.T) {
	dirA, dirB, _, _, cancel := twoEngines(t)
	defer cancel()

	big := strings.Repeat("a", 300*1024) // > 256 KB cap
	if err := os.WriteFile(filepath.Join(dirA, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dirB, "big.txt")); err == nil {
		t.Fatal("oversize file should not have synced")
	}
}

func TestGitignoredFileNotSynced(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, ".gitignore"), []byte("secrets.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hub := transport.NewMemHub()
	tA := hub.New("peer-A")
	tB := hub.New("peer-B")
	sA := store.New()
	sB := store.New()
	defer sA.Close()
	defer sB.Close()
	eA := New(sA, tA, "peer-A", dirA)
	eB := New(sB, tB, "peer-B", dirB)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eA.Run(ctx)
	go eB.Run(ctx)
	time.Sleep(150 * time.Millisecond)

	if err := os.WriteFile(filepath.Join(dirA, "secrets.env"), []byte("KEY=top"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dirB, "secrets.env")); err == nil {
		t.Fatal("gitignored file should not have synced")
	}
}
