package main

import "github.com/charmbracelet/lipgloss"

var (
	// Neon operations-console palette.
	colorBg       = lipgloss.Color("#080b14")
	colorFg       = lipgloss.Color("#d7e2ff")
	colorAccent   = lipgloss.Color("#51e5ff") // Cyan
	colorActive   = lipgloss.Color("#c084fc") // Violet
	colorBorder   = lipgloss.Color("#263552") // Steel blue
	colorMuted    = lipgloss.Color("#6d7ea8")
	colorGood     = lipgloss.Color("#5eead4") // Mint
	colorBad      = lipgloss.Color("#fb7185") // Coral
	colorWarning  = lipgloss.Color("#fbbf24") // Amber
	colorHeaderBg = lipgloss.Color("#0d1324")
	colorPanelBg  = lipgloss.Color("#0b1120")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorFg).
			Background(lipgloss.Color("#111a2e")).
			Padding(0, 1)

	inactivePanelStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(colorBorder).
				Background(colorPanelBg).
				Padding(0, 1)

	activePanelStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()). // Thick border for active panel
				BorderForeground(colorActive).
				Background(colorPanelBg).
				Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	okStyle  = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	badStyle = lipgloss.NewStyle().Foreground(colorBad).Bold(true)

	spinnerStyle = lipgloss.NewStyle().Foreground(colorAccent)

	logErrorStyle   = lipgloss.NewStyle().Foreground(colorBad).Bold(true)
	logWarnStyle    = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	logSuccessStyle = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	logInfoStyle    = lipgloss.NewStyle().Foreground(colorAccent)
	logURLStyle     = lipgloss.NewStyle().Foreground(colorActive).Underline(true)
	logHTTPStyle    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	logStatusStyle  = lipgloss.NewStyle().Foreground(colorGood).Bold(true)

	// Header bar: project :: branch shown top-right
	headerBranchStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent).
				Background(colorHeaderBg).
				Padding(0, 1)

	brandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorFg).
			Background(colorHeaderBg).
			Padding(0, 1)

	headerMetaStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Background(colorHeaderBg).
			Padding(0, 1)

	sectionLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent).
				Background(colorPanelBg).
				Padding(0, 1)

	badgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBg).
			Background(colorGood).
			Padding(0, 1)

	keyCapStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBg).
			Background(colorAccent).
			Padding(0, 1)

	dockLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMuted).
			Padding(0, 1)

	mascotStyle = lipgloss.NewStyle().
			Foreground(colorActive).
			Bold(true)

	mascotCaptionStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				PaddingLeft(5)

	// Log panel inner title: "{Action} logs"
	logPanelTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMuted).
				Padding(0, 0, 0, 1)

	// Menu overlay styles (LazyGit-style)
	menuBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(colorAccent).
			Background(colorPanelBg).
			Padding(1, 2)

	menuKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent) // Blue accent for key column

	menuDescStyle = lipgloss.NewStyle().
			Foreground(colorFg) // White fg for description column

	menuSepStyle = lipgloss.NewStyle().
			Foreground(colorBorder) // Muted separator between key and desc
)
