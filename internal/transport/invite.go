package transport

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// Invite-code format (M1):
//
//	COLLAB-<peer_id_hex_64>-<token_hex_10>
//
// 64 hex chars for the full ed25519 peer ID + 10 hex chars (40 bits) of
// random token entropy. Long, but copy-pasteable in one line. Future: chunk
// for readability or shorten via base32.
const invitePrefix = "COLLAB-"

// NewInvite mints a fresh invite for the given peer ID.
func NewInvite(peerID string) (Invite, error) {
	if len(peerID) != 64 {
		return Invite{}, fmt.Errorf("peer ID must be 64-char hex, got %d", len(peerID))
	}
	var tokBytes [5]byte
	if _, err := rand.Read(tokBytes[:]); err != nil {
		return Invite{}, fmt.Errorf("token entropy: %w", err)
	}
	tok := hex.EncodeToString(tokBytes[:])
	return Invite{
		Code:   invitePrefix + peerID + "-" + tok,
		PeerID: peerID,
		Token:  tok,
	}, nil
}

// ParseInvite decodes an invite code into its parts.
func ParseInvite(code string) (Invite, error) {
	if !strings.HasPrefix(code, invitePrefix) {
		return Invite{}, fmt.Errorf("invite must start with %s", invitePrefix)
	}
	rest := code[len(invitePrefix):]
	parts := strings.Split(rest, "-")
	if len(parts) != 2 {
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
	return Invite{Code: code, PeerID: peerID, Token: tok}, nil
}
