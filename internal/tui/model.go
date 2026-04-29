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

// Snapshot is the read-only view the TUI needs each tick. The collab CLI
// supplies a closure that builds one from live state. The TUI never imports
// store / transport — just this struct.
type Snapshot struct {
	SessionID string
	Role      string // "host" | "joiner"
	MyName    string
	Invite    string // empty for joiner

	Peers   []Peer
	Entries []LogEntry
	Files   []File
}

type Peer struct {
	Name     string
	ID       string
	Self     bool
	JoinedAt time.Time
	Status   string // current "what I'm working on" line; empty = no status
}

// LogEntry is one line in the shared conversation log.
type LogEntry struct {
	Timestamp  time.Time
	PeerID     string
	PeerName   string // resolved from peer table; empty if unknown
	Role       string // "user" | "assistant"
	Content    string
	Kind       string // "" | "post" | "question" | "answer"
	TargetPeer string // for questions
}

// File is one row in the shared-files pane.
type File struct {
	Path     string
	Size     int64
	ModTime  time.Time
	PeerID   string
	PeerName string
}

// Config wires the model to live state and to the action that launches the
// AI agent.
type Config struct {
	SnapshotFn    func() Snapshot
	AgentLauncher func() *exec.Cmd
	AgentName     string
	OnQuit        func()
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
	logScrl int // 0 = pinned to newest; > 0 = scrolled up by N entries
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

func (m *model) Init() tea.Cmd { return tick() }

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

		case "j", "down":
			if m.logScrl > 0 {
				m.logScrl--
			}
			return m, nil
		case "k", "up":
			m.logScrl++
			return m, nil
		case "G":
			m.logScrl = 0
			return m, nil

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

const (
	minInnerWidth = 60
	twoColCutoff  = 100 // inner width above which peers + log go side-by-side
	// styFrame is RoundedBorder (1) + Padding(1,2) (2) on each side = 6 cols
	// of horizontal overhead. The outer Padding(1,2) wrap adds another 4.
	frameOverhead = 6
	outerOverhead = 4
)

// pickHeights decides how tall the log + files panes should grow so the TUI
// fills the available terminal height instead of stranding empty rows below.
func (m *model) pickHeights(reservedRows int) (logH int) {
	// Total non-pane rows: header (1), blanks (~6), invite (2), peers (~3),
	// files (~2), footer (1). Plus reservedRows for outer padding/border.
	const fixedRows = 14
	avail := m.height - fixedRows - reservedRows
	if avail < 6 {
		return 6
	}
	if avail > 24 {
		return 24
	}
	return avail
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	frameW := m.width - outerOverhead
	if frameW < minInnerWidth+frameOverhead {
		frameW = minInnerWidth + frameOverhead
	}
	innerW := frameW - frameOverhead

	logH := m.pickHeights(0)

	var sections []string
	sections = append(sections, m.renderHeader(innerW))
	sections = append(sections, "")
	sections = append(sections, m.renderInvite(innerW))

	if innerW >= twoColCutoff {
		colW := innerW/2 - 1
		left := m.renderPeers(colW)
		right := m.renderLogSized(colW, logH)
		sections = append(sections, "")
		sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right))
	} else {
		sections = append(sections, "")
		sections = append(sections, m.renderPeers(innerW))
		sections = append(sections, "")
		sections = append(sections, m.renderLogSized(innerW, logH))
	}

	sections = append(sections, "")
	sections = append(sections, m.renderFiles(innerW))
	sections = append(sections, "")
	sections = append(sections, m.renderFooter(innerW))

	body := strings.Join(sections, "\n")
	framed := styFrame.Width(frameW).Render(body)
	return lipgloss.NewStyle().Padding(1, 2).Render(framed)
}

// header — top bar with brand, session name, role, identity (right-aligned).
func (m *model) renderHeader(w int) string {
	left := styBold.Render("collab") +
		styFaint.Render("  ·  ") +
		styAccB.Render(m.snap.SessionID) +
		styFaint.Render("  ·  ") +
		styDim.Render(m.snap.Role)
	right := styDim.Render("you are ") + styAccent.Render(m.snap.MyName)
	return spaceBetween(left, right, w)
}

// renderSection joins a section header (▎ LABEL) with its body content.
func (m *model) renderSection(label, body string) string {
	header := styMarker.Render("▎ ") + stySectionLabel.Render(strings.ToUpper(label))
	return header + "\n" + indent(body, 2)
}

func (m *model) renderInvite(_ int) string {
	if m.snap.Invite == "" {
		return m.renderSection("session", styFaint.Render("(joined — no invite to share)"))
	}
	hint := styFaint.Render("  press ") + styKeyHint.Render("[c]") + styFaint.Render(" to copy")
	body := styAccent.Render(m.snap.Invite) + hint
	return m.renderSection("invite", body)
}

func (m *model) renderPeers(_ int) string {
	label := fmt.Sprintf("peers (%d)", len(m.snap.Peers))
	if len(m.snap.Peers) == 0 {
		return m.renderSection(label, styFaint.Render("(none yet — share the invite to add a peer)"))
	}
	var lines []string
	for _, p := range sortedPeers(m.snap.Peers) {
		dot := styPeerOther.Render("●")
		name := styAccent.Render(displayName(p))
		if p.Self {
			dot = styPeerSelf.Render("●")
			name = styPeerSelf.Render(displayName(p))
		}
		var meta string
		if p.Self {
			meta = styFaint.Render("you")
		} else {
			meta = styFaint.Render("joined " + relTime(p.JoinedAt))
		}
		row := dot + "  " + name + styFaint.Render("  · ") + meta
		if p.Status != "" {
			row += "\n   " + styText.Render(p.Status)
		}
		lines = append(lines, row)
	}
	return m.renderSection(label, strings.Join(lines, "\n"))
}

// renderLogSized renders the log pane reserving roughly maxRows of body
// space — entries beyond that scroll. _ is the column hint (unused, but
// keeps the signature shape consistent).
func (m *model) renderLogSized(_ , maxRows int) string {
	if maxRows < 4 {
		maxRows = 4
	}
	label := fmt.Sprintf("log (%d)", len(m.snap.Entries))
	if len(m.snap.Entries) == 0 {
		// Fill empty space with vertical pad so the section reserves its
		// height rather than collapsing to one line.
		empty := styFaint.Render("(no messages yet — agent posts via the post_to_log tool show here)")
		filler := strings.Repeat("\n", maxRows-1)
		return m.renderSection(label, empty+filler)
	}
	all := m.snap.Entries
	end := len(all) - m.logScrl
	if end < 0 {
		end = 0
	}
	start := end - maxRows
	if start < 0 {
		start = 0
	}
	view := all[start:end]
	var lines []string
	for _, e := range view {
		when := e.Timestamp.Local().Format("15:04")
		who := e.PeerName
		if who == "" {
			who = shortPeerID(e.PeerID)
		}
		whoStyled := styAccent.Render(who)
		if isYou(e, m.snap) {
			whoStyled = styPeerSelf.Render("you")
		}
		// Render kind tag for non-default entries.
		var tag string
		switch e.Kind {
		case "question":
			tag = " " + styAccB.Render("?→"+e.TargetPeer)
		case "answer":
			tag = " " + styPeerSelf.Render("←answer")
		}
		head := styFaint.Render(when) + "  " + whoStyled + tag + styFaint.Render(":")
		lines = append(lines, head+" "+styText.Render(strings.TrimSpace(e.Content)))
	}
	for len(lines) < maxRows {
		lines = append(lines, "")
	}
	if m.logScrl > 0 {
		lines = append(lines, styFaint.Render(fmt.Sprintf("(scrolled %d back · press G to jump to newest)", m.logScrl)))
	}
	return m.renderSection(label, strings.Join(lines, "\n"))
}

func (m *model) renderFiles(_ int) string {
	label := fmt.Sprintf("shared files (%d)", len(m.snap.Files))
	if len(m.snap.Files) == 0 {
		return m.renderSection(label, styFaint.Render("(empty — files in ./shared/ propagate to every peer)"))
	}
	var lines []string
	files := append([]File(nil), m.snap.Files...)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		who := f.PeerName
		if who == "" {
			who = shortPeerID(f.PeerID)
		}
		ago := relTime(f.ModTime)
		row := styText.Render(f.Path) +
			styFaint.Render("  · "+humanSize(f.Size)+
				"  · from "+who+" "+ago)
		lines = append(lines, row)
	}
	return m.renderSection(label, strings.Join(lines, "\n"))
}

func (m *model) renderFooter(w int) string {
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
	if len(m.snap.Entries) > 0 {
		keys = append(keys, styKeyHint.Render("[j/k]")+" "+styKeyDesc.Render("scroll log"))
	}
	keys = append(keys, styKeyHint.Render("[q]")+" "+styKeyDesc.Render("quit"))

	left := strings.Join(keys, styFaint.Render("    "))
	right := ""
	if m.status != "" {
		right = styStatusOK.Render(m.status)
	}
	return spaceBetween(left, right, w)
}

// ───────── helpers ─────────

func spaceBetween(left, right string, w int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := max(w-lw-rw, 1)
	return left + strings.Repeat(" ", gap) + right
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
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
	return shortPeerID(p.ID)
}

func shortPeerID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:8] + "…" + id[len(id)-4:]
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

func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

func isYou(e LogEntry, s Snapshot) bool {
	for _, p := range s.Peers {
		if p.Self && p.ID == e.PeerID {
			return true
		}
	}
	return false
}

// copyToClipboard tries pbcopy / wl-copy / xclip / xsel — first one found wins.
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
