package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Aman035/collab-ai/internal/handle"
	"github.com/Aman035/collab-ai/internal/mcp"
	"github.com/Aman035/collab-ai/internal/state"
	"github.com/Aman035/collab-ai/internal/store"
	collabsync "github.com/Aman035/collab-ai/internal/sync"
	"github.com/Aman035/collab-ai/internal/transport"
	"github.com/Aman035/collab-ai/internal/tui"
)

func createCmd() *cobra.Command {
	var name, agent string
	var detach bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Start a new pairing session and launch your AI agent",
		Long: "Hosts a new collab-ai session under ~/collab-ai/<id>/, exposes\n" +
			"an HTTP MCP server, writes a child .mcp.json next to the session\n" +
			"dir, and opens a TUI from which you can launch your AI agent.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			return runCreate(ctx, name, agent, detach)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Your handle (default: auto-generated)")
	cmd.Flags().StringVar(&agent, "agent", "claude", "AI agent to launch (currently only \"claude\")")
	cmd.Flags().BoolVar(&detach, "detach", false, "Skip the TUI and launch the agent immediately")
	return cmd
}

func connectCmd() *cobra.Command {
	var name, agent string
	var detach bool
	cmd := &cobra.Command{
		Use:   "connect <invite-code>",
		Short: "Join an existing pairing session and launch your AI agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signalContext()
			defer cancel()
			return runConnect(ctx, args[0], name, agent, detach)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Your handle (default: auto-generated)")
	cmd.Flags().StringVar(&agent, "agent", "claude", "AI agent to launch (currently only \"claude\")")
	cmd.Flags().BoolVar(&detach, "detach", false, "Skip the TUI and launch the agent immediately")
	return cmd
}

func runCreate(ctx context.Context, name, agent string, detach bool) error {
	sessionID, sharedDir, err := allocateSessionDir()
	if err != nil {
		return err
	}
	if name == "" {
		name = pickHandle()
	}

	ax := transport.NewAxlTransport()
	ax.SetIdentity(name, sessionID)
	fmt.Fprintln(os.Stderr, "→ bringing up Axl daemon...")
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

	mcpURL, mcpStop, err := startMCPHTTP(ctx, st, ax.PeerID())
	if err != nil {
		return err
	}
	defer mcpStop()

	sessionRoot := filepath.Dir(sharedDir)
	if err := writeChildMCPConfig(sessionRoot, mcpURL); err != nil {
		return err
	}
	if err := writeSessionPrompt(sessionRoot, sessionID, name, "host", invite.Code); err != nil {
		return err
	}
	if err := persistInvite(sessionRoot, invite.Code); err != nil {
		return err
	}

	if detach {
		printCreateBanner(invite, sessionID, name, sharedDir, mcpURL)
	}

	sess := newSession("host", ax.PeerID(), sharedDir, invite.Code, st, ax)
	sess.id = sessionID
	sess.myName = name
	go sess.trackPeerEvents(func(ev transport.PeerEvent) {
		logPeerEvent(ev)
		if ev.Kind == transport.PeerJoined {
			_ = engine.Replay(ev.Peer.ID)
		}
	})

	stateStop := make(chan struct{})
	go state.NewWriter(sess.snapshot, time.Second).Run(stateStop)
	defer close(stateStop)

	if detach {
		return launchAgent(ctx, agent, sessionRoot)
	}
	return runTUI(ctx, agent, sessionRoot, sess)
}

func runConnect(ctx context.Context, code, name, agent string, detach bool) error {
	invite, err := transport.ParseInvite(code)
	if err != nil {
		return fmt.Errorf("invite: %w", err)
	}
	sessionID, sharedDir, err := allocateSessionDir()
	if err != nil {
		return err
	}
	if name == "" {
		name = pickHandle()
	}

	ax := transport.NewAxlTransport()
	ax.SetIdentity(name, "")
	fmt.Fprintln(os.Stderr, "→ bringing up Axl daemon and joining...")
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

	mcpURL, mcpStop, err := startMCPHTTP(ctx, st, ax.PeerID())
	if err != nil {
		return err
	}
	defer mcpStop()

	sessionRoot := filepath.Dir(sharedDir)
	if err := writeChildMCPConfig(sessionRoot, mcpURL); err != nil {
		return err
	}
	if err := writeSessionPrompt(sessionRoot, sessionID, name, "joiner", ""); err != nil {
		return err
	}

	if detach {
		printConnectBanner(invite, sessionID, name, sharedDir, mcpURL)
	}

	sess := newSession("joiner", ax.PeerID(), sharedDir, "", st, ax)
	sess.id = sessionID
	sess.myName = name
	go sess.trackPeerEvents(logPeerEvent)

	// Adopt the host's session ID once hello_ack arrives.
	go func() {
		for range 100 {
			if id := ax.SessionID(); id != "" && id != sess.id {
				sess.id = id
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	stateStop := make(chan struct{})
	go state.NewWriter(sess.snapshot, time.Second).Run(stateStop)
	defer close(stateStop)

	if detach {
		return launchAgent(ctx, agent, sessionRoot)
	}
	return runTUI(ctx, agent, sessionRoot, sess)
}

// runTUI opens the bubbletea session view. The user presses [a] to launch
// the configured agent in the foreground (TUI suspends, agent runs, TUI
// resumes when the agent exits) and [q] to quit.
func runTUI(ctx context.Context, agent, sessionRoot string, sess *session) error {
	tuiCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	return tui.Run(tuiCtx, tui.Config{
		AgentName: agent,
		SnapshotFn: func() tui.Snapshot {
			return sessionToTUI(sess)
		},
		AgentLauncher: func() *exec.Cmd {
			cmd := exec.CommandContext(tuiCtx, agentBinary(agent))
			cmd.Dir = sessionRoot
			return cmd
		},
		OnQuit: cancel,
	})
}

// agentBinary resolves the binary path for the chosen agent. Returns "claude"
// as a fallback so exec.Command surfaces a useful PATH error if it's missing.
func agentBinary(agent string) string {
	switch agent {
	case "", "claude":
		if bin, err := exec.LookPath("claude"); err == nil {
			return bin
		}
		return "claude"
	default:
		return agent
	}
}

// sessionToTUI shapes the session into the snapshot the TUI consumes.
func sessionToTUI(sess *session) tui.Snapshot {
	sess.mu.Lock()
	peers := make([]tui.Peer, 0, len(sess.peers)+1)
	peers = append(peers, tui.Peer{
		Name: sess.myName, ID: sess.peerID, Self: true, JoinedAt: sess.startedAt,
	})
	for id, p := range sess.peers {
		peers = append(peers, tui.Peer{
			Name: p.Name, ID: id, JoinedAt: p.JoinedAt,
		})
	}
	sess.mu.Unlock()

	return tui.Snapshot{
		SessionID: sess.id,
		Role:      sess.role,
		MyName:    sess.myName,
		Invite:    sess.inviteCode,
		Peers:     peers,
		LogCount:  len(sess.store.EntriesSince(time.Time{})),
		FileCount: len(sess.store.ListFiles("")),
	}
}

// pickHandle resolves the user's handle: $COLLAB_NAME wins if set,
// otherwise we generate a friendly one.
func pickHandle() string {
	if v := os.Getenv("COLLAB_NAME"); v != "" {
		return v
	}
	return handle.New()
}

// startMCPHTTP starts the MCP HTTP server on a free local port and returns
// the URL plus a cleanup func.
func startMCPHTTP(ctx context.Context, st *store.Store, peerID string) (url string, stop func(), err error) {
	port, err := freeLocalPort()
	if err != nil {
		return "", nil, fmt.Errorf("pick mcp port: %w", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	mcpURL := fmt.Sprintf("http://%s/mcp", addr)

	srv := mcp.New(st, peerID)
	srvCtx, cancel := context.WithCancel(ctx)
	go func() { _ = srv.ServeHTTP(srvCtx, addr) }()
	return mcpURL, cancel, nil
}

// writeChildMCPConfig drops a .mcp.json next to the session dir so the
// agent (when launched with cwd=sessionRoot) auto-discovers our HTTP MCP.
func writeChildMCPConfig(sessionRoot, mcpURL string) error {
	cfg := fmt.Sprintf(`{
  "mcpServers": {
    "collab-ai": {
      "type": "http",
      "url": "%s"
    }
  }
}
`, mcpURL)
	path := filepath.Join(sessionRoot, ".mcp.json")
	return os.WriteFile(path, []byte(cfg), 0o600)
}

// writeSessionPrompt drops a CLAUDE.md into the session dir so the launched
// agent reads it as project context at session start. Tells the agent it's
// in a pairing session, what tools are available, and when to use them — so
// the user doesn't have to nudge "use the post_to_log tool" by hand.
func writeSessionPrompt(sessionRoot, sessionID, myHandle, role, inviteCode string) error {
	const promptTmpl = `# collab-ai pairing session

You are the AI agent for **%[1]s** in a live pairing session
(` + "`%[2]s`" + `, role: ` + "`%[3]s`" + `) with another developer who's
using their own AI agent through collab-ai.

The ` + "`collab-ai`" + ` MCP server is connected — three tools are available
and you should use them **proactively** without being asked:

## Tools and when to reach for them

- **` + "`get_shared_log`" + `** — call at the start of any new task to catch
  up on prior context, and again when the user references something that
  may have come from the other side. Returns all entries chronologically.
- **` + "`post_to_log`" + `** — share findings, decisions, plans, or insights
  with the partner's agent. Treat it like a group chat: post when something
  changes shared understanding. Skip filler.
- **` + "`list_shared_files`" + `** — list files in ` + "`./shared/`" + `, the
  workspace that auto-syncs between peers. Files outside ` + "`./shared/`" + `
  are local-only.

## Conventions

- Files you create in ` + "`./shared/`" + ` propagate to every peer within a
  few seconds. Anything elsewhere stays on this machine.
- Run ` + "`get_shared_log`" + ` first thing — it's how you join an
  in-progress conversation rather than starting cold.
- Use role=` + "`assistant`" + ` when sharing your own reasoning, role=` + "`user`" + `
  when echoing what the human typed verbatim.

## Right now

- Session: ` + "`%[2]s`" + `
- Your handle: ` + "`%[1]s`" + `
- Your role: ` + "`%[3]s`" + `
- Shared directory: ` + "`./shared/`" + `
%[4]s`

	var inviteSection string
	if inviteCode != "" {
		inviteSection = "- Invite code (share to add another peer): `" + inviteCode + "`\n"
	}
	body := fmt.Sprintf(promptTmpl, myHandle, sessionID, role, inviteSection)
	path := filepath.Join(sessionRoot, "CLAUDE.md")
	return os.WriteFile(path, []byte(body), 0o644)
}

// persistInvite writes the invite code to <sessionRoot>/INVITE so it's
// recoverable after Claude Code's TUI clears the screen.
func persistInvite(sessionRoot, inviteCode string) error {
	if inviteCode == "" {
		return nil
	}
	path := filepath.Join(sessionRoot, "INVITE")
	return os.WriteFile(path, []byte(inviteCode+"\n"), 0o600)
}

// launchAgent execs the user's chosen agent in the session dir so it picks
// up the .mcp.json we just wrote. Blocks until the agent exits.
func launchAgent(ctx context.Context, agent, cwd string) error {
	switch agent {
	case "", "claude":
		bin, err := exec.LookPath("claude")
		if err != nil {
			return fmt.Errorf("claude binary not on PATH; install Claude Code first (https://claude.com/claude-code)")
		}
		cmd := exec.CommandContext(ctx, bin)
		cmd.Dir = cwd
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil && ctx.Err() == nil {
			return fmt.Errorf("agent exited: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown agent %q (only \"claude\" is supported in v0.1)", agent)
	}
}

func freeLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func printCreateBanner(invite transport.Invite, sessionID, name, sharedDir, mcpURL string) {
	c := newPalette(os.Stderr)
	w := os.Stderr
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.bold("collab-ai")+c.dim(" — created session ")+c.accent(sessionID))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  You are:     ")+c.accent(name))
	fmt.Fprintln(w, c.dim("  Shared dir:  ")+sharedDir)
	fmt.Fprintln(w, c.dim("  MCP server:  ")+mcpURL)
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Invite for collaborators:"))
	fmt.Fprintln(w, "    "+c.accent(invite.Code))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Launching agent…  (close it to end the session)"))
	fmt.Fprintln(w)
}

func printConnectBanner(invite transport.Invite, sessionID, name, sharedDir, mcpURL string) {
	c := newPalette(os.Stderr)
	w := os.Stderr
	_ = sessionID
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.bold("collab-ai")+c.dim(" — joining session"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  You are:     ")+c.accent(name))
	fmt.Fprintln(w, c.dim("  Host peer:   ")+shortPeer(invite.PeerID))
	fmt.Fprintln(w, c.dim("  Shared dir:  ")+sharedDir)
	fmt.Fprintln(w, c.dim("  MCP server:  ")+mcpURL)
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim("  Launching agent…  (close it to leave the session)"))
	fmt.Fprintln(w)
}
