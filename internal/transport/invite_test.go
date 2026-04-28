package transport

import (
	"strings"
	"testing"
)

func TestInviteRoundtrip(t *testing.T) {
	pid := strings.Repeat("ab", 32) // 64 hex chars
	inv, err := NewInvite(pid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(inv.Code, invitePrefix) {
		t.Fatalf("code prefix: %q", inv.Code)
	}
	got, err := ParseInvite(inv.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got.PeerID != pid {
		t.Fatalf("peer ID: got %q want %q", got.PeerID, pid)
	}
	if got.Token != inv.Token {
		t.Fatalf("token: got %q want %q", got.Token, inv.Token)
	}
}

func TestInviteRejectsMalformed(t *testing.T) {
	cases := []string{
		"BADPREFIX-aaaaaa-bbbbbb",
		"COLLAB-tooShort-bbbbbbbbbb",
		"COLLAB-" + strings.Repeat("a", 64) + "-shortok",
		"COLLAB-" + strings.Repeat("z", 64) + "-1234567890",      // not hex
		"COLLAB-" + strings.Repeat("a", 64) + "-zzzzzzzzzz",      // not hex
	}
	for _, c := range cases {
		if _, err := ParseInvite(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}
