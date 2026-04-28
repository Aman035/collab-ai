// Live two-Claude-Code demo (simulated).
//
// Spawns two collab-ai processes — host and joiner — and drives each with a
// real MCP client over stdio (the exact transport Claude Code uses). Then
// exercises every M1/M2/M3 acceptance gate from the build plan:
//
//   1. log entry written via MCP on host -> visible via MCP on joiner
//   2. log entry written via MCP on joiner -> visible via MCP on host
//   3. file written into host's ./shared/ -> appears in joiner's ./shared/
//   4. list_shared_files MCP tool on joiner sees the file
//   5. `collab-ai status` from a third process reflects the running session
//
// This is the "what would happen if two Claude Codes were paired" smoke test,
// without needing two real interactive Claude Code sessions.
//
// Prereq: PATH must include the Axl `node` binary (or COLLAB_AXL_NODE set).
// Run: `PATH="/path/to/axl:$PATH" go run .` from this directory.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	demoTimeout    = 90 * time.Second
	syncTimeout    = 8 * time.Second
	fileSyncTimeout = 8 * time.Second
)

var inviteRe = regexp.MustCompile(`COLLAB-[a-f0-9]+-[a-f0-9]+`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ demo failed:", err)
		os.Exit(1)
	}
	fmt.Println()
	fmt.Println("✓ all demo checks passed")
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), demoTimeout)
	defer cancel()

	if _, err := exec.LookPath("node"); err != nil {
		if os.Getenv("COLLAB_AXL_NODE") == "" {
			return fmt.Errorf("axl 'node' binary not on PATH; set PATH or COLLAB_AXL_NODE")
		}
	}

	binary, err := buildCollabAI()
	if err != nil {
		return fmt.Errorf("build collab-ai: %w", err)
	}
	fmt.Println("→ built", binary)

	tmp, err := os.MkdirTemp("", "collab-demo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	hostDir := filepath.Join(tmp, "host", "shared")
	joinDir := filepath.Join(tmp, "join", "shared")
	_ = os.MkdirAll(hostDir, 0o755)
	_ = os.MkdirAll(joinDir, 0o755)

	// ---- HOST ----
	fmt.Println("→ starting host (collab-ai host)...")
	hostC, hostStderr, err := spawn(binary, "host", "--dir", hostDir)
	if err != nil {
		return fmt.Errorf("spawn host: %w", err)
	}
	defer hostC.Close()

	invite, err := waitForInvite(hostStderr, 20*time.Second)
	if err != nil {
		return fmt.Errorf("read invite: %w", err)
	}
	fmt.Println("→ invite:", abbrev(invite))

	if _, err := initialize(ctx, hostC); err != nil {
		return fmt.Errorf("host MCP initialize: %w", err)
	}

	// ---- JOINER ----
	fmt.Println("→ starting joiner (collab-ai join)...")
	joinC, joinStderr, err := spawn(binary, "join", invite, "--dir", joinDir)
	if err != nil {
		return fmt.Errorf("spawn joiner: %w", err)
	}
	defer joinC.Close()

	if err := waitForJoinedLine(joinStderr, 15*time.Second); err != nil {
		return fmt.Errorf("joiner ready: %w", err)
	}
	fmt.Println("→ joiner connected")

	if _, err := initialize(ctx, joinC); err != nil {
		return fmt.Errorf("joiner MCP initialize: %w", err)
	}

	// ============================================================
	// CHECK 1: post_to_log on host -> visible on joiner
	// ============================================================
	msgA := "hello from the host side"
	if err := postToLog(ctx, hostC, "user", msgA); err != nil {
		return fmt.Errorf("post_to_log on host: %w", err)
	}
	if err := waitForLogContent(ctx, joinC, msgA, syncTimeout); err != nil {
		return fmt.Errorf("joiner did not see host's entry: %w", err)
	}
	fmt.Println("✓ check 1: host log entry visible on joiner via MCP")

	// ============================================================
	// CHECK 2: post_to_log on joiner -> visible on host
	// ============================================================
	msgB := "and back from the joiner"
	if err := postToLog(ctx, joinC, "assistant", msgB); err != nil {
		return fmt.Errorf("post_to_log on joiner: %w", err)
	}
	if err := waitForLogContent(ctx, hostC, msgB, syncTimeout); err != nil {
		return fmt.Errorf("host did not see joiner's entry: %w", err)
	}
	fmt.Println("✓ check 2: joiner log entry visible on host via MCP")

	// ============================================================
	// CHECK 3: file written on host -> appears on joiner
	// ============================================================
	hostFile := filepath.Join(hostDir, "hello.txt")
	if err := os.WriteFile(hostFile, []byte("greetings from host"), 0o644); err != nil {
		return err
	}
	joinFile := filepath.Join(joinDir, "hello.txt")
	if err := waitForFile(joinFile, "greetings from host", fileSyncTimeout); err != nil {
		return fmt.Errorf("joiner did not receive file: %w", err)
	}
	fmt.Println("✓ check 3: file written on host appears on joiner")

	// ============================================================
	// CHECK 4: list_shared_files on joiner sees the file
	// ============================================================
	res, err := callTool(ctx, joinC, "list_shared_files", nil)
	if err != nil {
		return fmt.Errorf("list_shared_files: %w", err)
	}
	if !strings.Contains(toolResultText(res), "hello.txt") {
		return fmt.Errorf("list_shared_files missing hello.txt:\n%s", toolResultText(res))
	}
	fmt.Println("✓ check 4: list_shared_files on joiner reports hello.txt")

	// ============================================================
	// CHECK 5: collab-ai status reflects active session
	// ============================================================
	out, err := exec.Command(binary, "status").Output()
	if err != nil {
		return fmt.Errorf("collab-ai status: %w", err)
	}
	s := string(out)
	if !strings.Contains(s, "host") || !strings.Contains(s, "Peers:") {
		return fmt.Errorf("status output unexpected:\n%s", s)
	}
	if !strings.Contains(s, "Log entries:") {
		return fmt.Errorf("status missing entries count:\n%s", s)
	}
	fmt.Println("✓ check 5: collab-ai status reflects the live session")

	return nil
}

// ───────── helpers ─────────

func buildCollabAI() (string, error) {
	out := filepath.Join(os.TempDir(), "collab-ai-demo")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/collab-ai")
	cmd.Dir = mustRepoRoot()
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out, nil
}

// mustRepoRoot returns the parent project root by walking up from this
// experiment's directory until it finds a go.mod whose module path is
// exactly the parent module (not this experiment's own go.mod).
func mustRepoRoot() string {
	wd, _ := os.Getwd()
	wantLine := "module github.com/Aman035/collab-ai\n"
	for d := wd; d != "/"; d = filepath.Dir(d) {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err != nil {
			continue
		}
		if strings.HasPrefix(string(b), wantLine) {
			return d
		}
	}
	panic("could not find parent repo root from " + wd)
}

func spawn(binary string, args ...string) (*client.Client, <-chan string, error) {
	stdio := transport.NewStdio(binary, nil, args...)
	c := client.NewClient(stdio)
	if err := c.Start(context.Background()); err != nil {
		return nil, nil, err
	}
	stderrCh := streamStderr(stdio)
	return c, stderrCh, nil
}

// streamStderr forwards each stderr line to a channel and (best-effort) also
// to our own stderr so the human running the demo can see what's happening.
func streamStderr(s *transport.Stdio) <-chan string {
	out := make(chan string, 256)
	go func() {
		defer close(out)
		r := s.Stderr()
		if r == nil {
			return
		}
		sc := bufio.NewScanner(r)
		var mu sync.Mutex
		for sc.Scan() {
			line := sc.Text()
			mu.Lock()
			fmt.Fprintln(os.Stderr, "  │ "+line)
			mu.Unlock()
			select {
			case out <- line:
			default:
			}
		}
	}()
	return out
}

func waitForInvite(ch <-chan string, timeout time.Duration) (string, error) {
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return "", fmt.Errorf("stderr closed")
			}
			if m := inviteRe.FindString(line); m != "" {
				return m, nil
			}
		case <-deadline:
			return "", fmt.Errorf("timeout waiting for invite")
		}
	}
}

func waitForJoinedLine(ch <-chan string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return fmt.Errorf("stderr closed")
			}
			if strings.Contains(line, "joined session") || strings.Contains(line, "Point your AI agent") {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("timeout waiting for joiner ready")
		}
	}
}

func initialize(ctx context.Context, c *client.Client) (mcp.InitializeResult, error) {
	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: "collab-ai-demo", Version: "0"}
	res, err := c.Initialize(ctx, req)
	if err != nil {
		return mcp.InitializeResult{}, err
	}
	return *res, nil
}

func postToLog(ctx context.Context, c *client.Client, role, content string) error {
	_, err := callTool(ctx, c, "post_to_log", map[string]any{
		"role":    role,
		"content": content,
	})
	return err
}

func callTool(ctx context.Context, c *client.Client, name string, args map[string]any) (*mcp.CallToolResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	return c.CallTool(ctx, req)
}

func toolResultText(r *mcp.CallToolResult) string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range r.Content {
		if t, ok := c.(mcp.TextContent); ok {
			b.WriteString(t.Text)
			b.WriteByte('\n')
		}
	}
	if r.StructuredContent != nil {
		out, _ := json.Marshal(r.StructuredContent)
		b.Write(out)
	}
	return b.String()
}

func waitForLogContent(ctx context.Context, c *client.Client, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := callTool(ctx, c, "get_shared_log", nil)
		if err == nil && strings.Contains(toolResultText(res), want) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("log entry containing %q never appeared within %v", abbrev(want), timeout)
}

func waitForFile(path, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), want) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("file %s never matched %q within %v", path, abbrev(want), timeout)
}

func abbrev(s string) string {
	if len(s) <= 32 {
		return s
	}
	return s[:14] + "…" + s[len(s)-14:]
}

// keep io.Reader import alive (not actually used at top level after refactor)
var _ = io.EOF
