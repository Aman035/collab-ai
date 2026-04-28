package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/internal/state"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show info about the running collab-ai session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus()
		},
	}
}

func runStatus() error {
	f, err := state.Read()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "no active collab-ai session")
			os.Exit(1)
		}
		return err
	}

	c := newPalette(os.Stdout)

	fmt.Println()
	header := c.bold("collab-ai") + " " + c.dim("v"+f.Version)
	if f.SessionID != "" {
		header += "  " + c.dim("session:") + " " + c.accent(f.SessionID)
	}
	fmt.Println(header)
	fmt.Println()

	row := func(label, value string) {
		fmt.Printf("  %-15s %s\n", c.dim(label), value)
	}
	row("Role:", f.Role)
	if f.InviteCode != "" {
		row("Invite:", c.accent(f.InviteCode))
	}
	row("Started:", humanDuration(time.Since(f.StartedAt))+" ago")
	row("Shared dir:", f.SharedDir)
	row("Log entries:", fmt.Sprintf("%d", len(f.Entries)))
	row("Files in sync:", fmt.Sprintf("%d", len(f.Files)))
	fmt.Println()

	// Peer table.
	peers := append([]state.Peer(nil), f.Peers...)
	sort.SliceStable(peers, func(i, j int) bool {
		// self first, then by joined_at
		if peers[i].Self != peers[j].Self {
			return peers[i].Self
		}
		return peers[i].JoinedAt.Before(peers[j].JoinedAt)
	})

	fmt.Println(c.dim("  Peers:"))
	for _, p := range peers {
		tag := "peer  "
		note := "joined " + humanDuration(time.Since(p.JoinedAt)) + " ago"
		if p.Self {
			tag = "self  "
			note = "you (" + f.Role + ")"
		}
		display := p.Name
		if display == "" {
			display = shortPeer(p.ID)
		}
		fmt.Printf("    %s %-18s   %s\n", c.accent(tag), display, c.dim(note))
	}
	fmt.Println()
	return nil
}

// shortPeer renders a 64-hex pubkey as the first 12 chars + ellipsis + last 4.
func shortPeer(id string) string {
	if len(id) <= 20 {
		return id
	}
	return id[:12] + "…" + id[len(id)-4:]
}

// humanDuration formats a Duration in compact human form: "12s", "3m", "1h 14m".
func humanDuration(d time.Duration) string {
	if d < time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
