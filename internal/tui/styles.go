// Package tui hosts the bubbletea/lipgloss TUI for collab-ai sessions.
// Aesthetic stays consistent with the landing page: warm-dark base,
// sodium-amber accent, paper-cream text, mono headlines.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette — single source of truth for every color in the TUI.
var (
	colAccent     = lipgloss.Color("215") // 256-color sodium amber, matches the web landing
	colText       = lipgloss.Color("230") // paper cream
	colTextDim    = lipgloss.Color("245")
	colTextFaint  = lipgloss.Color("240")
	colBorder     = lipgloss.Color("236")
	colSelfMarker = lipgloss.Color("41") // a quiet green for "you"
)

var (
	styBold  = lipgloss.NewStyle().Bold(true).Foreground(colText)
	styDim   = lipgloss.NewStyle().Foreground(colTextDim)
	styFaint = lipgloss.NewStyle().Foreground(colTextFaint)

	styAccent     = lipgloss.NewStyle().Foreground(colAccent)
	styAccentBold = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	styCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1)

	styCardTitle = lipgloss.NewStyle().
			Foreground(colTextDim).
			Padding(0, 1).
			MarginRight(1)

	styBanner = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	styKeyHint = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)
	styKeyDesc = lipgloss.NewStyle().
			Foreground(colTextDim)

	styPeerSelf  = lipgloss.NewStyle().Foreground(colSelfMarker)
	styPeerOther = lipgloss.NewStyle().Foreground(colAccent)

	styStatusOK = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
)
