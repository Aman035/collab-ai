package tui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Snapshot is the read-only view the TUI needs each tick. The collab-ai CLI
// supplies a closure that builds one from live state (store + transport's
// peer table). Keeping the dep one-way means the TUI doesn't import store
// or transport.
type Snapshot struct {
	SessionID string
	Role      string // "host" | "joiner"
	MyName    string
	Invite    string // empty for joiner
	Peers     []Peer
	LogCount  int
	FileCount int
}

type Peer struct {
	Name     string
	ID       string
	Self     bool
	JoinedAt time.Time
}

// Config wires the model to live state and to the action that launches the
// AI agent.
type Config struct {
	// SnapshotFn returns the latest snapshot. Called every tick.
	SnapshotFn func() Snapshot

	// AgentLauncher returns a configured exec.Cmd for the AI agent. The TUI
	// suspends, runs it in the foreground, then resumes.
	AgentLauncher func() *exec.Cmd

	// AgentName is what we call the agent in keybind labels ("launch claude").
	AgentName string

	// OnQuit, if set, runs when the user quits the TUI. Used to cancel the
	// session context.
	OnQuit func()
}

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context, cfg Config) error {
	m := newModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	go func() {
		<-ctx.Done()
		p.Quit()
	}()
	_, err := p.Run()
	return err
}

// ───────── model ─────────

type model struct {
	cfg     Config
	snap    Snapshot
	width   int
	height  int
	status  string
	statTTL time.Time
}

func newModel(cfg Config) *model {
	m := &model{cfg: cfg}
	if cfg.SnapshotFn != nil {
		m.snap = cfg.SnapshotFn()
	}
	return m
}

type tickMsg time.Time
type clearStatusMsg struct{}
type agentDoneMsg struct{ err error }

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearStatusMsg{} })
}

func (m *model) Init() tea.Cmd {
	return tick()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		if m.cfg.SnapshotFn != nil {
			m.snap = m.cfg.SnapshotFn()
		}
		return m, tick()

	case clearStatusMsg:
		m.status = ""
		return m, nil

	case agentDoneMsg:
		if msg.err != nil {
			m.status = "agent exited: " + msg.err.Error()
		} else {
			m.status = "agent exited."
		}
		return m, clearStatusAfter(3 * time.Second)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			if m.cfg.OnQuit != nil {
				m.cfg.OnQuit()
			}
			return m, tea.Quit

		case "c":
			if m.snap.Invite != "" {
				if err := copyToClipboard(m.snap.Invite); err != nil {
					m.status = "copy failed (no pbcopy?)"
				} else {
					m.status = "invite copied."
				}
				return m, clearStatusAfter(2 * time.Second)
			}

		case "a":
			if m.cfg.AgentLauncher == nil {
				return m, nil
			}
			cmd := m.cfg.AgentLauncher()
			if cmd == nil {
				m.status = "launcher returned nil"
				return m, clearStatusAfter(2 * time.Second)
			}
			return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
				return agentDoneMsg{err: err}
			})
		}
	}
	return m, nil
}

// ───────── view ─────────

func (m *model) View() string {
	if m.width == 0 {
		return "" // first frame before SizeMsg
	}

	header := m.renderHeader()
	identity := m.renderIdentity()
	invite := m.renderInvite()
	peers := m.renderPeers()
	stats := m.renderStats()
	footer := m.renderFooter()

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		identity,
		"",
		invite,
		"",
		peers,
		"",
		stats,
		"",
		footer,
	)

	// Center horizontally with comfortable max width.
	maxW := min(m.width-4, 96)
	wrapped := lipgloss.NewStyle().Width(maxW).Render(body)
	return lipgloss.NewStyle().Padding(1, 2).Render(wrapped)
}

func (m *model) renderHeader() string {
	left := styBanner.Render("collab-ai")
	mid := styDim.Render(" — session ")
	name := styAccentBold.Render(m.snap.SessionID)
	role := styFaint.Render("  · " + m.snap.Role)
	return left + mid + name + role
}

func (m *model) renderIdentity() string {
	return styDim.Render("  You are: ") + styAccent.Render(m.snap.MyName)
}

func (m *model) renderInvite() string {
	if m.snap.Invite == "" {
		return styCard.Width(80).Render(styDim.Render("joined session — no invite to share"))
	}
	title := styCardTitle.Render("invite")
	body := styAccent.Render(m.snap.Invite)
	hint := styFaint.Render(" press [c] to copy")
	content := body + "\n" + hint
	card := styCard.Render(content)
	return overlayTitle(card, title)
}

func (m *model) renderPeers() string {
	title := styCardTitle.Render(fmt.Sprintf("peers (%d)", len(m.snap.Peers)))
	if len(m.snap.Peers) == 0 {
		return overlayTitle(styCard.Width(60).Render(styFaint.Render("(none yet)")), title)
	}
	rows := make([]string, 0, len(m.snap.Peers))
	for _, p := range sortedPeers(m.snap.Peers) {
		marker := styPeerOther.Render("●")
		label := styAccent.Render(displayName(p))
		note := ""
		if p.Self {
			marker = styPeerSelf.Render("●")
			label = styPeerSelf.Render(displayName(p))
			note = styFaint.Render("  (you)")
		} else {
			note = styFaint.Render("  joined " + relTime(p.JoinedAt))
		}
		rows = append(rows, marker+" "+label+note)
	}
	return overlayTitle(styCard.Render(strings.Join(rows, "\n")), title)
}

func (m *model) renderStats() string {
	parts := []string{
		styDim.Render("log entries: ") + styBold.Render(fmt.Sprintf("%d", m.snap.LogCount)),
		styDim.Render("files: ") + styBold.Render(fmt.Sprintf("%d", m.snap.FileCount)),
	}
	if m.status != "" {
		parts = append(parts, styStatusOK.Render(m.status))
	}
	return "  " + strings.Join(parts, styFaint.Render("   ·   "))
}

func (m *model) renderFooter() string {
	agent := m.cfg.AgentName
	if agent == "" {
		agent = "agent"
	}
	keys := []string{
		styKeyHint.Render("[a]") + " " + styKeyDesc.Render("launch "+agent),
	}
	if m.snap.Invite != "" {
		keys = append(keys, styKeyHint.Render("[c]")+" "+styKeyDesc.Render("copy invite"))
	}
	keys = append(keys, styKeyHint.Render("[q]")+" "+styKeyDesc.Render("quit"))
	return "  " + strings.Join(keys, styFaint.Render("   "))
}

// overlayTitle puts a small label on the top border of a card. Lipgloss
// doesn't have native title support, so we render the card and splice the
// title into the top border line.
func overlayTitle(card, title string) string {
	lines := strings.Split(card, "\n")
	if len(lines) == 0 {
		return card
	}
	top := lines[0]
	plain := lipgloss.Width(title)
	// Replace a slice of the top border, preserving leading corner char.
	if lipgloss.Width(top) < plain+4 {
		return card
	}
	first := []rune(top)[0]
	lines[0] = string(first) + " " + title + top[lipgloss.Width(string(first))+lipgloss.Width(" "+title):]
	return strings.Join(lines, "\n")
}

func sortedPeers(in []Peer) []Peer {
	out := append([]Peer(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Self != out[j].Self {
			return out[i].Self
		}
		return out[i].JoinedAt.Before(out[j].JoinedAt)
	})
	return out
}

func displayName(p Peer) string {
	if p.Name != "" {
		return p.Name
	}
	if len(p.ID) > 16 {
		return p.ID[:12] + "…" + p.ID[len(p.ID)-4:]
	}
	return p.ID
}

func relTime(t time.Time) string {
	d := time.Since(t)
	if d < time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// copyToClipboard tries the platform's clipboard tool. macOS = pbcopy,
// Linux = xclip / wl-copy. Best-effort; returns nil on success.
func copyToClipboard(s string) error {
	for _, candidate := range [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	} {
		bin, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, candidate[1:]...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			continue
		}
		if err := cmd.Start(); err != nil {
			continue
		}
		_, _ = stdin.Write([]byte(s))
		_ = stdin.Close()
		if err := cmd.Wait(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no clipboard tool found")
}
