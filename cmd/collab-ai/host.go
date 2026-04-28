package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/internal/handle"
	"github.com/Aman035/collab-ai/internal/mcp"
	"github.com/Aman035/collab-ai/internal/state"
	"github.com/Aman035/collab-ai/internal/store"
	collabsync "github.com/Aman035/collab-ai/internal/sync"
	"github.com/Aman035/collab-ai/internal/transport"
)

func hostCmd() *cobra.Command {
	var dir, name string
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Start a session as the host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			return runHost(ctx, dir, name)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Directory to share (default: ~/collab-ai/<session>/shared/)")
	cmd.Flags().StringVar(&name, "name", "", "Your handle (default: a friendly auto-generated one)")
	return cmd
}

func runHost(ctx context.Context, dir, name string) error {
	sessionID, sharedDir, err := resolveDir(dir)
	if err != nil {
		return err
	}
	if name == "" {
		if v := os.Getenv("COLLAB_NAME"); v != "" {
			name = v
		} else {
			name = handle.New()
		}
	}

	ax := transport.NewAxlTransport()
	ax.SetIdentity(name, sessionID)
	fmt.Fprintln(os.Stderr, "collab-ai — bringing up Axl daemon...")
	invite, err := ax.Host(ctx)
	if err != nil {
		return fmt.Errorf("host: %w", err)
	}
	defer ax.Close()

	st := store.New()
	defer st.Close()

	engine := collabsync.New(st, ax, ax.PeerID(), sharedDir)
	go func() {
		if err := engine.Run(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "sync engine stopped:", err)
		}
	}()

	printHostBanner(invite, sharedDir, sessionID, name)

	sess := newSession("host", ax.PeerID(), sharedDir, invite.Code, st, ax)
	sess.id = sessionID
	sess.myName = name
	go sess.trackPeerEvents(logPeerEvent)

	stateStop := make(chan struct{})
	go state.NewWriter(sess.snapshot, time.Second).Run(stateStop)
	defer close(stateStop)

	mcpServer := mcp.New(st, ax.PeerID())
	return mcpServer.ServeStdio(ctx)
}

func printHostBanner(invite transport.Invite, sharedDir, sessionID, name string) {
	c := newPalette(os.Stderr)
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.bold("collab-ai")+c.dim(" — hosting session ")+c.accent(sessionID))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  You are:     ")+c.accent(name))
	fmt.Fprintln(w, c.dim("  Shared dir:  ")+sharedDir)
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Invite code:"))
	fmt.Fprintln(w, "    "+c.accent(invite.Code))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Share the invite with collaborators."))
	fmt.Fprintln(w, c.dim("  Point your AI agent at this binary as an MCP server."))
	fmt.Fprintln(w)
}

func logPeerEvent(ev transport.PeerEvent) {
	c := newPalette(os.Stderr)
	display := ev.Peer.Name
	if display == "" {
		display = shortPeer(ev.Peer.ID)
	}
	switch ev.Kind {
	case transport.PeerJoined:
		fmt.Fprintln(os.Stderr, c.accent("▸ peer joined: ")+display)
	case transport.PeerLeft:
		fmt.Fprintln(os.Stderr, c.dim("▸ peer left:   ")+display)
	}
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
