package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Aman035/colabAI/internal/mcp"
	"github.com/Aman035/colabAI/internal/store"
	collabsync "github.com/Aman035/colabAI/internal/sync"
	"github.com/Aman035/colabAI/internal/transport"
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

	engine := collabsync.New(st, ax, ax.PeerID())
	go func() {
		if err := engine.Run(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "sync engine stopped:", err)
		}
	}()

	printHostBanner(invite, dir)

	go logPeerEvents(ax)

	mcpServer := mcp.New(st, ax.PeerID())
	return mcpServer.ServeStdio(ctx)
}

func printHostBanner(invite transport.Invite, dir string) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "collab-ai — hosting session")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Invite code:")
	fmt.Fprintln(os.Stderr, "    "+invite.Code)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Shared dir:  "+dir)
	fmt.Fprintln(os.Stderr, "  Peer ID:     "+invite.PeerID)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Share the invite with collaborators.")
	fmt.Fprintln(os.Stderr, "  Point your AI agent at this binary as an MCP server.")
	fmt.Fprintln(os.Stderr)
}

func logPeerEvents(ax *transport.AxlTransport) {
	for ev := range ax.Events() {
		switch ev.Kind {
		case transport.PeerJoined:
			fmt.Fprintln(os.Stderr, "▸ peer joined:", ev.Peer.ID)
		case transport.PeerLeft:
			fmt.Fprintln(os.Stderr, "▸ peer left:  ", ev.Peer.ID)
		}
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
