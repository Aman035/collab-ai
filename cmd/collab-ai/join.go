package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/internal/handle"
	"github.com/Aman035/collab-ai/internal/mcp"
	"github.com/Aman035/collab-ai/internal/state"
	"github.com/Aman035/collab-ai/internal/store"
	collabsync "github.com/Aman035/collab-ai/internal/sync"
	"github.com/Aman035/collab-ai/internal/transport"
)

func joinCmd() *cobra.Command {
	var dir, name string
	cmd := &cobra.Command{
		Use:   "join <invite-code>",
		Short: "Join a session as a peer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			return runJoin(ctx, args[0], dir, name)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Directory to sync into (default: ~/collab-ai/<session>/shared/)")
	cmd.Flags().StringVar(&name, "name", "", "Your handle (default: a friendly auto-generated one)")
	return cmd
}

func runJoin(ctx context.Context, code, dir, name string) error {
	invite, err := transport.ParseInvite(code)
	if err != nil {
		return fmt.Errorf("invite: %w", err)
	}
	_, sharedDir, err := resolveDir(dir)
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
	ax.SetIdentity(name, "") // session ID arrives via hello_ack
	fmt.Fprintln(os.Stderr, "collab-ai — bringing up Axl daemon and joining...")
	if err := ax.Join(ctx, invite); err != nil {
		return fmt.Errorf("join: %w", err)
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

	c := newPalette(os.Stderr)
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.bold("collab-ai")+c.dim(" — joining..."))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  You are:    ")+c.accent(name))
	fmt.Fprintln(w, c.dim("  Host peer:  ")+shortPeer(invite.PeerID))
	fmt.Fprintln(w, c.dim("  Shared dir: ")+sharedDir)
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Point your AI agent at this binary as an MCP server."))
	fmt.Fprintln(w)

	sess := newSession("joiner", ax.PeerID(), sharedDir, "", st, ax)
	sess.myName = name
	go sess.trackPeerEvents(logPeerEvent)

	// As soon as hello_ack arrives, the transport adopts the host's
	// session ID. Poll once a second for ~10s and stamp it onto the
	// snapshot so status shows the same session name on both sides.
	go func() {
		for range 100 {
			if id := ax.SessionID(); id != "" {
				sess.id = id
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	stateStop := make(chan struct{})
	go state.NewWriter(sess.snapshot, time.Second).Run(stateStop)
	defer close(stateStop)

	mcpServer := mcp.New(st, ax.PeerID())
	return mcpServer.ServeStdio(ctx)
}
