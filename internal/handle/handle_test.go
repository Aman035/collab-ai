package handle

import (
	"strings"
	"testing"
)

func TestNewShape(t *testing.T) {
	for range 50 {
		got := New()
		parts := strings.Split(got, "-")
		if len(parts) != 2 {
			t.Fatalf("expected adjective-animal, got %q", got)
		}
		if parts[0] == "" || parts[1] == "" {
			t.Fatalf("empty part in %q", got)
		}
	}
}

func TestNewUniqueAvoidsCollision(t *testing.T) {
	used := map[string]struct{}{}
	for range 20 {
		n := NewUnique(func(s string) bool {
			_, ok := used[s]
			return ok
		})
		if _, dup := used[n]; dup {
			t.Fatalf("NewUnique returned a name we said was taken: %q", n)
		}
		used[n] = struct{}{}
	}
}

func TestNewUniqueWithExhaustedSpaceFallsBack(t *testing.T) {
	// Force every basic name to be reported as taken; the fallback should
	// produce a numbered variant we accept.
	got := NewUnique(func(s string) bool {
		return !strings.Contains(s, "-2") && !strings.Contains(s, "-3")
	})
	if !strings.Contains(got, "-2") && !strings.Contains(got, "-3") {
		t.Fatalf("expected numeric fallback, got %q", got)
	}
}
