package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/internal/mcp"
	"github.com/Aman035/collab-ai/internal/state"
	"github.com/Aman035/collab-ai/internal/store"
	collabsync "github.com/Aman035/collab-ai/internal/sync"
	"github.com/Aman035/collab-ai/internal/transport"
)

func hostCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Start a session as the host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			return runHost(ctx, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "./shared", "Directory to share (M2: file sync; M1: ignored)")
	return cmd
}

func runHost(ctx context.Context, dir string) error {
	if err := ensureDir(dir); err != nil {
		return err
	}

	ax := transport.NewAxlTransport()
	fmt.Fprintln(os.Stderr, "collab-ai — bringing up Axl daemon...")
	invite, err := ax.Host(ctx)
	if err != nil {
		return fmt.Errorf("host: %w", err)
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

	printHostBanner(invite, dir)

	sess := newSession("host", ax.PeerID(), dir, invite.Code, st, ax)
	go sess.trackPeerEvents(logPeerEvent)

	stateStop := make(chan struct{})
	go state.NewWriter(sess.snapshot, time.Second).Run(stateStop)
	defer close(stateStop)

	mcpServer := mcp.New(st, ax.PeerID())
	return mcpServer.ServeStdio(ctx)
}

func printHostBanner(invite transport.Invite, dir string) {
	c := newPalette(os.Stderr)
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.bold("collab-ai")+c.dim(" — hosting session"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Invite code:"))
	fmt.Fprintln(w, "    "+c.accent(invite.Code))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Shared dir:  ")+dir)
	fmt.Fprintln(w, c.dim("  Peer ID:     ")+invite.PeerID)
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Share the invite with collaborators."))
	fmt.Fprintln(w, c.dim("  Point your AI agent at this binary as an MCP server."))
	fmt.Fprintln(w)
}

func logPeerEvent(ev transport.PeerEvent) {
	c := newPalette(os.Stderr)
	switch ev.Kind {
	case transport.PeerJoined:
		fmt.Fprintln(os.Stderr, c.accent("▸ peer joined: ")+shortPeer(ev.Peer.ID))
	case transport.PeerLeft:
		fmt.Fprintln(os.Stderr, c.dim("▸ peer left:   ")+shortPeer(ev.Peer.ID))
	}
}

func ensureDir(p string) error {
	if p == "" {
		return nil
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", p, err)
	}
	return nil
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
	}()
	return ctx, cancel
}
