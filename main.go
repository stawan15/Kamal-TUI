package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type panel int

const (
	panelDestinations panel = iota
	panelLogs
)

type destItem string

func (d destItem) Title() string {
	if d == "" {
		return "(default — no -d flag)"
	}
	return string(d)
}
func (d destItem) Description() string { return "config/deploy" + destSuffix(d) + ".yml" }
func (d destItem) FilterValue() string { return string(d) }

func destSuffix(d destItem) string {
	if d == "" {
		return ""
	}
	return "." + string(d)
}

type secretItem struct {
	key string
}

func (s secretItem) Title() string       { return s.key }
func (s secretItem) Description() string { return "********" }
func (s secretItem) FilterValue() string { return s.key }

type logLineMsg string
type logStreamClosedMsg struct{}
type cmdDoneMsg struct{ err error }

type model struct {
	width, height int

	activePanel panel

	destList list.Model
	verInput textinput.Model
	viewport viewport.Model
	spinner  spinner.Model

	selectedAction actionItem
	selectedDest   string

	running    bool
	statusLine string
	lastErr    error

	lineCh chan string
	doneCh chan error
	cancel context.CancelFunc

	outputBuf []string

	showVersionInput bool
	versionAction    actionItem

	// Menu overlay
	showMenu bool

	// Secrets Manager State
	showSecrets   bool
	addingSecret  bool
	stepSecretKey bool // true = key input, false = value input
	secList       list.Model
	secKeyIn      textinput.Model
	secValIn      textinput.Model

	// Confirmation State
	showConfirm bool
	confirmCmd  []string
	confirmAct  actionItem
	confirmDest string
	confirmVer  string

	// Header info
	projectName string
	gitBranch   string

	// Performance Dashboard
	showDashboard  bool
	dashStats      []ContainerStat
	dashErr        error
	dashLoading bool
}

// detectProjectName tries to get a short project name from the git remote URL
// or falls back to the current directory name.
func detectProjectName() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err == nil {
		remote := strings.TrimSpace(string(out))
		// strip .git suffix and take last path component
		remote = strings.TrimSuffix(remote, ".git")
		parts := strings.FieldsFunc(remote, func(r rune) bool {
			return r == '/' || r == ':'
		})
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	// fallback: current directory name
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Base(cwd)
	}
	return "kamal-tui"
}

// detectGitBranch returns the current git branch name.
func detectGitBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func initialModel() model {
	dests := discoverDestinations()
	ditems := make([]list.Item, 0, len(dests))
	for _, d := range dests {
		ditems = append(ditems, destItem(d))
	}
	dl := list.New(ditems, list.NewDefaultDelegate(), 0, 0)
	dl.Title = "Destinations"
	dl.SetShowStatusBar(false)
	dl.SetFilteringEnabled(false)
	dl.SetShowHelp(false)
	dl.Styles.Title = titleStyle

	ti := textinput.New()
	ti.Placeholder = "commit hash / version to roll back to"
	ti.CharLimit = 64
	ti.Prompt = "› "

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	vp := viewport.New(0, 0)

	secList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	secList.Title = "Secrets Manager"
	secList.SetShowStatusBar(false)
	secList.SetFilteringEnabled(false)
	secList.SetShowHelp(false)
	secList.Styles.Title = titleStyle

	secKeyIn := textinput.New()
	secKeyIn.Placeholder = "Secret Key (e.g. DATABASE_URL)"
	secKeyIn.Prompt = "Key: "

	secValIn := textinput.New()
	secValIn.Placeholder = "Secret Value"
	secValIn.Prompt = "Value: "
	secValIn.EchoMode = textinput.EchoPassword
	secValIn.EchoCharacter = '*'

	return model{
		activePanel: panelDestinations,
		destList:    dl,
		verInput:    ti,
		viewport:    vp,
		spinner:     sp,
		outputBuf:   []string{"Welcome to kamal-tui! Select a destination and press x for menu.", "Press 's' to manage secrets."},
		secList:     secList,
		secKeyIn:    secKeyIn,
		secValIn:    secValIn,
		projectName: detectProjectName(),
		gitBranch:   detectGitBranch(),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

// dashFetch runs pollDockerStats in a goroutine and returns the result as a Cmd.
// dest is the currently selected Kamal destination (empty = default).
func dashFetch(dest string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stats, err := pollDockerStats(ctx, dest)
		return dashRefreshMsg{stats: stats, err: err}
	}
}

// dashTick schedules the next auto-refresh after dashPollInterval.
func dashTick() tea.Cmd {
	return tea.Tick(dashPollInterval, func(t time.Time) tea.Msg {
		return dashTickMsg{}
	})
}

func waitForLine(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return logStreamClosedMsg{}
		}
		return logLineMsg(line)
	}
}

func waitForDone(ch <-chan error) tea.Cmd {
	return func() tea.Msg {
		err := <-ch
		return cmdDoneMsg{err: err}
	}
}

func (m *model) layout() {
	headerH := 1
	footerH := 1
	bodyH := m.height - headerH - footerH
	if bodyH < 3 {
		bodyH = 3
	}
	leftW := 30
	if m.width < 80 {
		leftW = m.width / 3
	}
	rightW := m.width - leftW

	// Destinations fills the full left column height
	m.destList.SetSize(leftW-4, bodyH-2)

	m.viewport.Width = rightW - 4
	m.viewport.Height = bodyH - 2

	m.secList.SetSize(m.width-10, m.height-6)
}

func (m *model) refreshSecrets() {
	keys := getSecretKeys()
	items := make([]list.Item, 0, len(keys))
	for _, k := range keys {
		items = append(items, secretItem{key: k})
	}
	m.secList.SetItems(items)
}

func (m model) handleActionByKey(key string) (tea.Model, tea.Cmd) {
	action, found := actionByKey(key)
	if !found {
		return m, nil
	}

	dest := ""
	if it, ok := m.destList.SelectedItem().(destItem); ok {
		dest = string(it)
	}

	if action.needsVersion {
		m.versionAction = action
		m.showVersionInput = true
		m.verInput.Focus()
		return m, textinput.Blink
	}

	return m.promptConfirm(action, dest, "")
}

func (m model) promptConfirm(action actionItem, dest, version string) (tea.Model, tea.Cmd) {
	m.showConfirm = true
	m.confirmAct = action
	m.confirmDest = dest
	m.confirmVer = version
	args := action.buildArgs(dest, version)
	m.confirmCmd = append([]string{"kamal"}, args...)
	return m, nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case dashRefreshMsg:
		m.dashLoading = false
		m.dashStats = msg.stats
		m.dashErr = msg.err
		if m.showDashboard {
			return m, dashTick()
		}
		return m, nil

	case dashTickMsg:
		if m.showDashboard {
			dest := ""
			if it, ok := m.destList.SelectedItem().(destItem); ok {
				dest = string(it)
			}
			return m, dashFetch(dest)
		}
		return m, nil

	case tea.MouseMsg:
		if m.showSecrets || m.addingSecret || m.showVersionInput || m.showConfirm || m.showMenu {
			return m, nil
		}
		leftW := 30
		if m.width < 80 {
			leftW = m.width / 3
		}
		if msg.X < leftW {
			m.activePanel = panelDestinations
			var cmd tea.Cmd
			m.destList, cmd = m.destList.Update(msg)
			cmds = append(cmds, cmd)
		} else {
			m.activePanel = panelLogs
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "q":
			if m.showDashboard {
				m.showDashboard = false
				return m, nil
			}
			if !m.showVersionInput && !m.running && !m.showSecrets && !m.addingSecret && !m.showConfirm && !m.showMenu {
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		case "esc":
			if m.showDashboard {
				m.showDashboard = false
				return m, nil
			}
			if m.showMenu {
				m.showMenu = false
				return m, nil
			}
			if m.showConfirm {
				m.showConfirm = false
				return m, nil
			}
			if m.addingSecret {
				m.addingSecret = false
				m.secKeyIn.Blur()
				m.secValIn.Blur()
				return m, nil
			}
			if m.showSecrets {
				m.showSecrets = false
				return m, nil
			}
			if m.showVersionInput {
				m.showVersionInput = false
				m.verInput.Blur()
				return m, nil
			}
			if m.running {
				break // use ctrl+c to abort
			}
		case "r":
			// Manual refresh when dashboard is open
			if m.showDashboard {
				m.dashLoading = true
				dest := ""
				if it, ok := m.destList.SelectedItem().(destItem); ok {
					dest = string(it)
				}
				return m, dashFetch(dest)
			}
		case "tab":
			if !m.showVersionInput && !m.showSecrets && !m.addingSecret && !m.showConfirm && !m.showMenu && !m.showDashboard {
				m.activePanel = (m.activePanel + 1) % 2
				return m, nil
			}
		case "shift+tab":
			if !m.showVersionInput && !m.showSecrets && !m.addingSecret && !m.showConfirm && !m.showMenu && !m.showDashboard {
				m.activePanel = (m.activePanel - 1 + 2) % 2
				return m, nil
			}
		}

		if m.showConfirm {
			switch msg.String() {
			case "y", "Y", "enter":
				m.showConfirm = false
				return m.startRun(m.confirmAct, m.confirmDest, m.confirmVer)
			case "n", "N", "q":
				m.showConfirm = false
				return m, nil
			}
			return m, nil
		}

		if m.addingSecret {
			switch msg.String() {
			case "enter":
				if m.stepSecretKey {
					key := strings.TrimSpace(m.secKeyIn.Value())
					if key != "" {
						m.stepSecretKey = false
						m.secKeyIn.Blur()
						m.secValIn.Focus()
						return m, textinput.Blink
					}
				} else {
					val := strings.TrimSpace(m.secValIn.Value())
					if val != "" {
						if err := addSecret(strings.TrimSpace(m.secKeyIn.Value()), val); err != nil {
							m.statusLine = badStyle.Render("failed to save secret: " + err.Error())
							return m, nil
						}
						m.addingSecret = false
						m.secKeyIn.Blur()
						m.secValIn.Blur()
						m.refreshSecrets()
						return m, nil
					}
				}
			default:
				var cmd tea.Cmd
				if m.stepSecretKey {
					m.secKeyIn, cmd = m.secKeyIn.Update(msg)
				} else {
					m.secValIn, cmd = m.secValIn.Update(msg)
				}
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		if m.showSecrets {
			switch msg.String() {
			case "a":
				m.addingSecret = true
				m.stepSecretKey = true
				m.secKeyIn.SetValue("")
				m.secValIn.SetValue("")
				m.secKeyIn.Focus()
				return m, textinput.Blink
			case "x", "d", "delete":
				if it, ok := m.secList.SelectedItem().(secretItem); ok {
					if err := removeSecret(it.key); err != nil {
						m.statusLine = badStyle.Render("failed to delete secret: " + err.Error())
						return m, nil
					}
					m.refreshSecrets()
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.secList, cmd = m.secList.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		if m.showVersionInput {
			switch msg.String() {
			case "enter":
				if strings.TrimSpace(m.verInput.Value()) != "" {
					ver := strings.TrimSpace(m.verInput.Value())
					m.showVersionInput = false

					dest := ""
					if it, ok := m.destList.SelectedItem().(destItem); ok {
						dest = string(it)
					}

					return m.promptConfirm(m.versionAction, dest, ver)
				}
			default:
				var cmd tea.Cmd
				m.verInput, cmd = m.verInput.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Menu overlay: handle action key presses
		if m.showMenu {
			key := msg.String()
			if _, found := actionByKey(key); found {
				m.showMenu = false
				return m.handleActionByKey(key)
			}
			// Unknown key — close menu
			m.showMenu = false
			return m, nil
		}

		// Normal mode shortcuts
		if !m.running {
			switch msg.String() {
			case "x":
				m.showMenu = true
				return m, nil
			case "s":
				m.showSecrets = true
				m.refreshSecrets()
				return m, nil
			case "p":
				// Open Performance Dashboard for selected destination
				m.showDashboard = true
				m.dashLoading = true
				dest := ""
				if it, ok := m.destList.SelectedItem().(destItem); ok {
					dest = string(it)
				}
				return m, tea.Batch(dashFetch(dest), dashTick())
			// Direct shortcuts (without opening menu)
			case "d":
				return m.handleActionByKey("d")
			}
		}

		// Panel specific updates
		if !m.showVersionInput && !m.showSecrets && !m.addingSecret && !m.showConfirm && !m.showMenu {
			switch m.activePanel {
			case panelDestinations:
				var cmd tea.Cmd
				m.destList, cmd = m.destList.Update(msg)
				cmds = append(cmds, cmd)
				if msg.String() == "enter" {
					m.activePanel = panelLogs
				}
			case panelLogs:
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}
		}

	case logLineMsg:
		m.outputBuf = append(m.outputBuf, string(msg))
		m.viewport.SetContent(strings.Join(m.outputBuf, "\n"))
		m.viewport.GotoBottom()
		return m, waitForLine(m.lineCh)

	case logStreamClosedMsg:
		return m, nil

	case cmdDoneMsg:
		m.running = false
		m.lastErr = msg.err
		if msg.err != nil {
			m.statusLine = badStyle.Render("failed: " + msg.err.Error())
		} else {
			m.statusLine = okStyle.Render("done")
		}
		return m, nil

	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m model) startRun(action actionItem, dest, version string) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.lineCh = make(chan string)
	m.doneCh = make(chan error, 1)
	m.running = true
	m.selectedAction = action
	m.outputBuf = nil
	m.statusLine = ""
	m.lastErr = nil
	m.verInput.Blur()

	m.verInput.SetValue("")
	args := action.buildArgs(dest, version)

	m.outputBuf = []string{"$ kamal " + strings.Join(args, " ")}

	go runKamal(ctx, dest, nil, args, m.lineCh, m.doneCh)

	m.viewport.SetContent(strings.Join(m.outputBuf, "\n"))
	return m, tea.Batch(m.spinner.Tick, waitForLine(m.lineCh), waitForDone(m.doneCh))
}

// headerView renders the top bar: empty left side + project::branch right-aligned.
func (m model) headerView() string {
	var label string
	if m.gitBranch != "" {
		label = m.projectName + " :: " + m.gitBranch
	} else {
		label = m.projectName
	}
	right := headerBranchStyle.Render(label)
	// Pad left so right label is flush right
	rightW := lipgloss.Width(right)
	padding := m.width - rightW
	if padding < 0 {
		padding = 0
	}
	return lipgloss.NewStyle().Background(colorHeaderBg).Width(m.width).Render(
		strings.Repeat(" ", padding) + right,
	)
}

// menuView renders the LazyGit-style centered menu overlay.
func (m model) menuView() string {
	items := actions()
	var rows []string

	for _, a := range items {
		key := menuKeyStyle.Render(fmt.Sprintf("%-3s", a.key))
		sep := menuSepStyle.Render("  ")
		desc := menuDescStyle.Render(a.title)
		rows = append(rows, key+sep+desc)
	}
	rows = append(rows, "") // blank separator
	rows = append(rows, menuKeyStyle.Render("esc")+"  "+menuDescStyle.Render("close"))

	inner := lipgloss.JoinVertical(lipgloss.Left, rows...)
	box := menuBoxStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Menu"),
			"",
			inner,
		),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	// ── Performance Dashboard overlay ──────────────────────────────────────
	if m.showDashboard {
		var content string
		if m.dashLoading && len(m.dashStats) == 0 {
			dest := ""
			if it, ok := m.destList.SelectedItem().(destItem); ok {
				dest = string(it)
			}
			destLabel := "default"
			if dest != "" {
				destLabel = dest
			}
			content = titleStyle.Render(fmt.Sprintf("󰐿  Container Performance  [dest: %s]", destLabel)) +
				"\n\n" + helpStyle.Render("  SSH-ing into remote servers and fetching docker stats…")
		} else {
			dest := ""
			if it, ok := m.destList.SelectedItem().(destItem); ok {
				dest = string(it)
			}
			content = renderDashboard(m.dashStats, m.dashErr, m.width, dest)
		}
		return activePanelStyle.
			Width(m.width - 4).
			Height(m.height - 4).
			Render(content)
	}

	// Menu overlay (highest priority after add-secret)
	if m.addingSecret {
		content := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Add New Secret"),
			"",
			m.secKeyIn.View(),
			"",
			m.secValIn.View(),
			"",
			helpStyle.Render("enter: next/save · esc: cancel"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, activePanelStyle.Width(50).Render(content))
	}
	if m.showSecrets {
		content := lipgloss.JoinVertical(lipgloss.Left,
			m.secList.View(),
			"",
			helpStyle.Render("a: add secret · x/d: delete · esc: back"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, activePanelStyle.Width(m.width-6).Height(m.height-2).Render(content))
	}
	if m.showConfirm {
		cmdStr := strings.Join(m.confirmCmd, " ")
		content := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Are you sure?"),
			"",
			lipgloss.NewStyle().Foreground(colorWarning).Render("This will run:"),
			lipgloss.NewStyle().Bold(true).Render("$ "+cmdStr),
			"",
			helpStyle.Render("Press 'y' to confirm, 'n' or 'esc' to cancel"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, activePanelStyle.Width(m.width-10).Render(content))
	}

	// ── Normal layout ─────────────────────────────────────────────────────
	leftW := 30
	if m.width < 80 {
		leftW = m.width / 3
	}
	rightW := m.width - leftW

	headerH := 1
	footerH := 1
	bodyH := m.height - headerH - footerH

	// Render Destinations (full left column height)
	style := inactivePanelStyle
	if m.activePanel == panelDestinations {
		style = activePanelStyle
	}
	destPanel := style.Width(leftW - 2).Height(bodyH - 2).Render(m.destList.View())

	// Render Logs panel
	style = inactivePanelStyle
	if m.activePanel == panelLogs {
		style = activePanelStyle
	}

	// Build log panel title: "{ActionName} logs" or just "logs"
	logTitle := "logs"
	if m.selectedAction.title != "" {
		clean := strings.TrimSpace(m.selectedAction.title)
		logTitle = clean + " logs"
	}
	logPanelTitle := logPanelTitleStyle.Render(logTitle)

	logContent := m.viewport.View()
	if m.showVersionInput {
		overlay := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Rollback version:"),
			"",
			m.verInput.View(),
		)
		logContent = lipgloss.Place(rightW-4, bodyH-4, lipgloss.Center, lipgloss.Center, activePanelStyle.Render(overlay))
	}

	logInner := lipgloss.JoinVertical(lipgloss.Left, logPanelTitle, logContent)
	logPanel := style.Width(rightW - 2).Height(bodyH - 2).Render(logInner)

	mainView := lipgloss.JoinHorizontal(lipgloss.Top, destPanel, logPanel)
	base := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), mainView, m.footerView())

	// Render menu overlay on top of base layout
	if m.showMenu {
		return m.menuView()
	}

	return base
}

func destLabel(d string) string {
	if d == "" {
		return "(default)"
	}
	return d
}

func (m model) footerView() string {
	var left string

	actionHint := ""
	if m.running {
		actionHint = m.spinner.View() + " running...  "
	}

	if m.statusLine != "" {
		actionHint += m.statusLine + "  "
	}
	left = actionHint + "d:deploy  p:dashboard  x:menu  s:secrets  tab:panel  q:quit"
	return statusBarStyle.Width(m.width).Render(left)
}

func main() {
	if _, _, ok := kamalBinaryAvailable(); !ok {
		fmt.Fprintln(os.Stderr, "warning: kamal binary not found yet (checked PATH, bin/kamal, bundle exec). Run this from your Rails project root, or install kamal first.")
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
