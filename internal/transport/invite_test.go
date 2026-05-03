package transport

import (
	"strings"
	"testing"
)

func TestInviteRoundtripWithAddr(t *testing.T) {
	pid := strings.Repeat("ab", 32)
	addr := "tls://server1.example.com:9001"
	inv, err := NewInvite(pid, addr)
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
	if got.Addr != addr {
		t.Fatalf("addr: got %q want %q", got.Addr, addr)
	}
}

func TestInviteEmptyAddrDefaultsToLoopback(t *testing.T) {
	pid := strings.Repeat("cd", 32)
	inv, err := NewInvite(pid, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseInvite(inv.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != DefaultPeerAddr {
		t.Fatalf("default addr: got %q want %q", got.Addr, DefaultPeerAddr)
	}
}

func TestInviteOldFormatStillParses(t *testing.T) {
	// Two-section invite (no addr block) should still parse and default to
	// loopback so existing single-machine demos keep working.
	pid := strings.Repeat("ef", 32)
	old := "COLLAB-" + pid + "-1234567890"
	got, err := ParseInvite(old)
	if err != nil {
		t.Fatal(err)
	}
	if got.PeerID != pid || got.Token != "1234567890" {
		t.Fatalf("parsed: %+v", got)
	}
	if got.Addr != DefaultPeerAddr {
		t.Fatalf("addr default: got %q", got.Addr)
	}
}

func TestInviteRejectsMalformed(t *testing.T) {
	cases := []string{
		"BADPREFIX-aaaaaa-bbbbbb",
		"COLLAB-tooShort-bbbbbbbbbb",
		"COLLAB-" + strings.Repeat("a", 64) + "-shortok",
		"COLLAB-" + strings.Repeat("z", 64) + "-1234567890",
		"COLLAB-" + strings.Repeat("a", 64) + "-zzzzzzzzzz",
	}
	for _, c := range cases {
		if _, err := ParseInvite(c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}
