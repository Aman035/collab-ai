package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// nodeBinary is the name of the Axl daemon executable. We exec whatever is
// on PATH; users who put it elsewhere can set COLLAB_AXL_NODE.
const nodeBinary = "node"

// tcpPortConstant must match across peers. M0 finding: this is internal to
// the gVisor stack on top of Yggdrasil; same value on every machine.
const tcpPortConstant = 7000

// daemonOpts captures the per-session config differences between host and
// joiner. The joiner uses Peers; the host uses Listen. tcp_port is fixed.
type daemonOpts struct {
	Listen []string
	Peers  []string
}

// daemon owns a child Axl `node` process and exposes a few derived fields
// (the peer ID it printed, the HTTP API URL) once /topology comes up.
type daemon struct {
	cmd     *exec.Cmd
	stateDir string
	apiURL  string
	peerID  string
}

// startDaemon spawns the Axl `node` binary with a generated config, then
// blocks until /topology returns 200 (or timeout).
func startDaemon(ctx context.Context, opts daemonOpts) (*daemon, error) {
	bin := os.Getenv("COLLAB_AXL_NODE")
	if bin == "" {
		bin = nodeBinary
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf(
			"axl 'node' binary not found on PATH (set COLLAB_AXL_NODE=/path/to/node, or build from https://github.com/gensyn-ai/axl)",
		)
	}

	apiPort, err := freeTCPPort()
	if err != nil {
		return nil, fmt.Errorf("pick api_port: %w", err)
	}

	stateDir, err := os.MkdirTemp("", "collab-ai-axl-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}

	cfg := map[string]any{
		"Listen":      opts.Listen,
		"Peers":       opts.Peers,
		"api_port":    apiPort,
		"bridge_addr": "127.0.0.1",
		"tcp_port":    tcpPortConstant,
	}
	cfgPath := filepath.Join(stateDir, "node-config.json")
	cfgBytes, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, cfgBytes, 0o600); err != nil {
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("write config: %w", err)
	}

	cmd := exec.CommandContext(ctx, bin, "-config", cfgPath)
	// Surface the daemon's stderr/stdout to ours so the user can see
	// public-key / peer connect lines. They're prefixed with [node] by Axl
	// already.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("start node: %w", err)
	}

	apiURL := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	d := &daemon{cmd: cmd, stateDir: stateDir, apiURL: apiURL}

	if err := d.waitReady(ctx, 10*time.Second); err != nil {
		_ = d.stop()
		return nil, err
	}
	return d, nil
}

// waitReady polls /topology until it returns 200 or the deadline expires.
// On success, d.peerID is populated.
func (d *daemon) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := http.Get(d.apiURL + "/topology")
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("topology: %s", resp.Status)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var t struct {
			OurPublicKey string `json:"our_public_key"`
		}
		if err := json.Unmarshal(body, &t); err != nil {
			lastErr = fmt.Errorf("parse topology: %w", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if t.OurPublicKey == "" {
			lastErr = errors.New("topology missing our_public_key")
			time.Sleep(200 * time.Millisecond)
			continue
		}
		d.peerID = t.OurPublicKey
		return nil
	}
	return fmt.Errorf("axl daemon never came up: %v", lastErr)
}

// stop signals the daemon to exit and cleans up.
func (d *daemon) stop() error {
	if d.cmd != nil && d.cmd.Process != nil {
		_ = d.cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- d.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = d.cmd.Process.Kill()
			<-done
		}
	}
	if d.stateDir != "" {
		os.RemoveAll(d.stateDir)
	}
	return nil
}

// freeTCPPort asks the OS for an unused localhost port.
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
