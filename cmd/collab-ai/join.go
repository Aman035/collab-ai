package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/internal/mcp"
	"github.com/Aman035/collab-ai/internal/state"
	"github.com/Aman035/collab-ai/internal/store"
	collabsync "github.com/Aman035/collab-ai/internal/sync"
	"github.com/Aman035/collab-ai/internal/transport"
)

func joinCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "join <invite-code>",
		Short: "Join a session as a peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			return runJoin(ctx, args[0], dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./shared", "Directory to sync into (M2: file sync; M1: ignored)")
	return cmd
}

func runJoin(ctx context.Context, code, dir string) error {
	invite, err := transport.ParseInvite(code)
	if err != nil {
		return fmt.Errorf("invite: %w", err)
	}
	if err := ensureDir(dir); err != nil {
		return err
	}

	ax := transport.NewAxlTransport()
	fmt.Fprintln(os.Stderr, "collab-ai — bringing up Axl daemon and joining...")
	if err := ax.Join(ctx, invite); err != nil {
		return fmt.Errorf("join: %w", err)
	}
	defer ax.Close()

	st := store.New()
	defer st.Close()

	engine := collabsync.New(st, ax, ax.PeerID(), dir)
	go func() {
		if err := engine.Run(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "sync engine stopped:", err)
		}
	}()

	c := newPalette(os.Stderr)
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.bold("collab-ai")+c.dim(" — joined session"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Host peer:  ")+shortPeer(invite.PeerID))
	fmt.Fprintln(w, c.dim("  My peer:    ")+shortPeer(ax.PeerID()))
	fmt.Fprintln(w, c.dim("  Shared dir: ")+dir)
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Point your AI agent at this binary as an MCP server."))
	fmt.Fprintln(w)

	sess := newSession("joiner", ax.PeerID(), dir, "", st, ax)
	go sess.trackPeerEvents(logPeerEvent)

	stateStop := make(chan struct{})
	go state.NewWriter(sess.snapshot, time.Second).Run(stateStop)
	defer close(stateStop)

	mcpServer := mcp.New(st, ax.PeerID())
	return mcpServer.ServeStdio(ctx)
}
