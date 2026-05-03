package transport

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Invite-code format:
//
//	COLLAB-<peer_id_hex_64>-<token_hex_10>[-<addr_b64url>]
//
// 64 hex chars for the full ed25519 peer ID + 10 hex chars (40 bits) of
// random token entropy + optional base64url-encoded TLS endpoint the
// joiner's Axl daemon should peer with (e.g. tls://server1.example.com:9001).
//
// Old two-section invites (no addr) still parse — they default to
// tls://127.0.0.1:9001 for backward compat with single-machine demos.
const (
	invitePrefix = "COLLAB-"

	// DefaultPeerAddr is what an old-format invite (no embedded addr)
	// implies — and what `collab create` picks when no --public-addr is
	// supplied. Single-machine loopback.
	DefaultPeerAddr = "tls://127.0.0.1:9001"
)

// NewInvite mints a fresh invite. addr is the host's reachable TLS endpoint
// (e.g. "tls://server1.example.com:9001"); empty addr falls back to the
// loopback default.
func NewInvite(peerID, addr string) (Invite, error) {
	if len(peerID) != 64 {
		return Invite{}, fmt.Errorf("peer ID must be 64-char hex, got %d", len(peerID))
	}
	if addr == "" {
		addr = DefaultPeerAddr
	}
	var tokBytes [5]byte
	if _, err := rand.Read(tokBytes[:]); err != nil {
		return Invite{}, fmt.Errorf("token entropy: %w", err)
	}
	tok := hex.EncodeToString(tokBytes[:])
	addrSection := base64.RawURLEncoding.EncodeToString([]byte(addr))
	return Invite{
		Code:   invitePrefix + peerID + "-" + tok + "-" + addrSection,
		PeerID: peerID,
		Token:  tok,
		Addr:   addr,
	}, nil
}

// ParseInvite decodes an invite code into its parts. Accepts both the
// old two-section format (COLLAB-<peerid>-<token>) and the current
// three-section format that embeds the host address.
func ParseInvite(code string) (Invite, error) {
	if !strings.HasPrefix(code, invitePrefix) {
		return Invite{}, fmt.Errorf("invite must start with %s", invitePrefix)
	}
	rest := code[len(invitePrefix):]
	// SplitN with limit=3 keeps the addr section intact even if base64url
	// itself contained a `-` (the URL-safe alphabet uses `-` and `_`).
	parts := strings.SplitN(rest, "-", 3)
	if len(parts) < 2 {
		return Invite{}, fmt.Errorf("invite code malformed")
	}
	peerID, tok := parts[0], parts[1]
	if len(peerID) != 64 {
		return Invite{}, fmt.Errorf("invite peer ID must be 64 hex chars, got %d", len(peerID))
	}
	if len(tok) != 10 {
		return Invite{}, fmt.Errorf("invite token must be 10 hex chars, got %d", len(tok))
	}
	if _, err := hex.DecodeString(peerID); err != nil {
		return Invite{}, fmt.Errorf("invite peer ID not hex: %w", err)
	}
	if _, err := hex.DecodeString(tok); err != nil {
		return Invite{}, fmt.Errorf("invite token not hex: %w", err)
	}
	addr := DefaultPeerAddr
	if len(parts) == 3 {
		raw, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			return Invite{}, fmt.Errorf("invite addr not base64url: %w", err)
		}
		addr = string(raw)
	}
	return Invite{Code: code, PeerID: peerID, Token: tok, Addr: addr}, nil
}
