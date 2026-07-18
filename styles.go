package main

import "github.com/charmbracelet/lipgloss"

var (
	// Tokyo Night-ish / Modern theme
	colorBg      = lipgloss.Color("#1a1b26")
	colorFg      = lipgloss.Color("#c0caf5")
	colorAccent  = lipgloss.Color("#7aa2f7") // Blue
	colorActive  = lipgloss.Color("#bb9af7") // Purple
	colorBorder  = lipgloss.Color("#414868") // Dark Gray
	colorMuted   = lipgloss.Color("#565f89")
	colorGood    = lipgloss.Color("#9ece6a") // Green
	colorBad     = lipgloss.Color("#f7768e") // Red
	colorWarning = lipgloss.Color("#e0af68") // Yellow

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorActive).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorFg).
			Background(lipgloss.Color("#24283b")). // slightly lighter bg
			Padding(0, 1)

	inactivePanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	activePanelStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()). // Thick border for active panel
			BorderForeground(colorActive).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	okStyle  = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	badStyle = lipgloss.NewStyle().Foreground(colorBad).Bold(true)

	spinnerStyle = lipgloss.NewStyle().Foreground(colorAccent)
)
