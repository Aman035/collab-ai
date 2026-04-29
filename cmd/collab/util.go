package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Aman035/collab-ai/internal/transport"
)

// signalContext returns a context cancelled on SIGINT or SIGTERM.
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

// logPeerEvent prints a single peer-join / peer-leave event to stderr.
// Used in --detach mode where there's no TUI to render the event.
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
