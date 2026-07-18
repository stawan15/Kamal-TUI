package main

import "github.com/charmbracelet/lipgloss"

var (
	colorAccent  = lipgloss.Color("205")
	colorMuted   = lipgloss.Color("241")
	colorGood    = lipgloss.Color("42")
	colorBad     = lipgloss.Color("197")
	colorBorder  = lipgloss.Color("62")
	colorActive  = lipgloss.Color("212")
	colorHeading = lipgloss.Color("212")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorHeading).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	inactivePanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	activePanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorActive).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	okStyle  = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	badStyle = lipgloss.NewStyle().Foreground(colorBad).Bold(true)

	spinnerStyle = lipgloss.NewStyle().Foreground(colorAccent)
)
