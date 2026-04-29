// Package tui hosts the bubbletea/lipgloss TUI for collab-ai sessions.
// Aesthetic stays consistent with the landing page: warm-dark base,
// sodium-amber accent, paper-cream text, mono headlines.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette — single source of truth for every color in the TUI.
var (
	colAccent     = lipgloss.Color("215") // 256-color sodium amber
	colText       = lipgloss.Color("230") // paper cream
	colTextDim    = lipgloss.Color("245")
	colTextFaint  = lipgloss.Color("240")
	colSelfMarker = lipgloss.Color("41") // a quiet green for "you"
)

var (
	styBold   = lipgloss.NewStyle().Bold(true).Foreground(colText)
	styText   = lipgloss.NewStyle().Foreground(colText)
	styDim    = lipgloss.NewStyle().Foreground(colTextDim)
	styFaint  = lipgloss.NewStyle().Foreground(colTextFaint)
	styAccent = lipgloss.NewStyle().Foreground(colAccent)
	styAccB   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	// Section label, e.g. "INVITE", "PEERS". Small caps style via uppercase.
	stySectionLabel = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	// Accent marker drawn before each section label (▎ = vertical bar).
	styMarker = lipgloss.NewStyle().Foreground(colAccent)

	styKeyHint = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styKeyDesc = lipgloss.NewStyle().Foreground(colTextDim)

	styPeerSelf  = lipgloss.NewStyle().Foreground(colSelfMarker)
	styPeerOther = lipgloss.NewStyle().Foreground(colAccent)

	styStatusOK = lipgloss.NewStyle().Foreground(colAccent).Italic(true)

	// Outer frame around the whole TUI.
	styFrame = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colTextFaint).
			Padding(1, 2)
)
