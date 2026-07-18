package main

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	panelActions
	panelLogs
)

// destItem wraps a destination name for the bubbles list component.
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

type logLineMsg string
type logStreamClosedMsg struct{}
type cmdDoneMsg struct{ err error }

type model struct {
	width, height int

	activePanel panel

	actionList list.Model
	destList   list.Model
	verInput   textinput.Model
	viewport   viewport.Model
	spinner    spinner.Model

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

	// Confirmation State
	showConfirm bool
	confirmCmd  []string
	confirmAct  actionItem
	confirmDest string
	confirmVer  string
}

func initialModel() model {
	items := make([]list.Item, 0, len(actions()))
	for _, a := range actions() {
		items = append(items, a)
	}
	al := list.New(items, list.NewDefaultDelegate(), 0, 0)
	al.Title = "Actions"
	al.SetShowStatusBar(false)
	al.SetFilteringEnabled(false)
	al.SetShowHelp(false)
	al.Styles.Title = titleStyle

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

	return model{
		activePanel: panelDestinations,
		actionList:  al,
		destList:    dl,
		verInput:    ti,
		viewport:    vp,
		spinner:     sp,
		outputBuf:   []string{"Welcome to kamal-tui! Select a destination and action."},
	}
}

func (m model) Init() tea.Cmd {
	return nil
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
	footerH := 1
	bodyH := m.height - footerH
	if bodyH < 3 {
		bodyH = 3
	}
	leftW := 30
	if m.width < 80 {
		leftW = m.width / 3
	}
	rightW := m.width - leftW

	destH := bodyH / 2
	actionH := bodyH - destH

	m.destList.SetSize(leftW-4, destH-2)
	m.actionList.SetSize(leftW-4, actionH-2)

	m.viewport.Width = rightW - 4
	m.viewport.Height = bodyH - 2
}

func (m model) handleShortcutAction(titleSubstr string) (tea.Model, tea.Cmd) {
	var action actionItem
	found := false
	for _, a := range actions() {
		if strings.Contains(a.title, titleSubstr) {
			action = a
			found = true
			break
		}
	}
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

	case tea.MouseMsg:
		if m.showVersionInput || m.showConfirm {
			return m, nil
		}
		leftW := 30
		if m.width < 80 {
			leftW = m.width / 3
		}
		if msg.X < leftW {
			destH := (m.height - 1) / 2
			if msg.Y < destH {
				m.activePanel = panelDestinations
				var cmd tea.Cmd
				m.destList, cmd = m.destList.Update(msg)
				cmds = append(cmds, cmd)
			} else {
				m.activePanel = panelActions
				var cmd tea.Cmd
				m.actionList, cmd = m.actionList.Update(msg)
				cmds = append(cmds, cmd)
			}
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
			if !m.showVersionInput && !m.running && !m.showConfirm {
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			}
		case "esc":
			if m.showConfirm {
				m.showConfirm = false
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
		case "tab":
			if !m.showVersionInput && !m.showConfirm {
				m.activePanel = (m.activePanel + 1) % 3
				return m, nil
			}
		case "shift+tab":
			if !m.showVersionInput && !m.showConfirm {
				m.activePanel = (m.activePanel - 1 + 3) % 3
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

		if !m.running {
			switch msg.String() {
			case "d":
				return m.handleShortcutAction("Deploy")
			case "r":
				return m.handleShortcutAction("Rollback")
			case "l":
				return m.handleShortcutAction("App Logs")
			}
		}

		// Panel specific updates
		if !m.showVersionInput && !m.showConfirm {
			switch m.activePanel {
			case panelDestinations:
				var cmd tea.Cmd
				m.destList, cmd = m.destList.Update(msg)
				cmds = append(cmds, cmd)
				if msg.String() == "enter" {
					m.activePanel = panelActions
				}
			case panelActions:
				var cmd tea.Cmd
				m.actionList, cmd = m.actionList.Update(msg)
				cmds = append(cmds, cmd)
				if msg.String() == "enter" {
					if it, ok := m.actionList.SelectedItem().(actionItem); ok {
						return m.handleShortcutAction(it.title)
					}
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
	m.outputBuf = nil
	m.statusLine = ""
	m.lastErr = nil
	m.verInput.Blur()

	m.verInput.SetValue("")
	args := action.buildArgs(dest, version)

	m.outputBuf = []string{"$ kamal " + strings.Join(args, " ")}

	go runKamal(ctx, nil, args, m.lineCh, m.doneCh)

	m.viewport.SetContent(strings.Join(m.outputBuf, "\n"))
	return m, tea.Batch(m.spinner.Tick, waitForLine(m.lineCh), waitForDone(m.doneCh))
}

func (m model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	// Render overlay if needed
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
	leftW := 30
	if m.width < 80 {
		leftW = m.width / 3
	}
	rightW := m.width - leftW

	footerH := 1
	bodyH := m.height - footerH

	destH := bodyH / 2
	actionH := bodyH - destH

	var destPanel, actionPanel, logPanel string

	// Render Destinations
	style := inactivePanelStyle
	if m.activePanel == panelDestinations {
		style = activePanelStyle
	}
	destPanel = style.Width(leftW - 2).Height(destH - 2).Render(m.destList.View())

	// Render Actions
	style = inactivePanelStyle
	if m.activePanel == panelActions {
		style = activePanelStyle
	}
	actionPanel = style.Width(leftW - 2).Height(actionH - 2).Render(m.actionList.View())

	// Render Logs
	style = inactivePanelStyle
	if m.activePanel == panelLogs {
		style = activePanelStyle
	}

	logContent := m.viewport.View()
	if m.showVersionInput {
		overlay := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Rollback version:"),
			"",
			m.verInput.View(),
		)
		logContent = lipgloss.Place(rightW-4, bodyH-4, lipgloss.Center, lipgloss.Center, overlay)
	}

	logPanel = style.Width(rightW - 2).Height(bodyH - 2).Render(logContent)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, destPanel, actionPanel)
	mainView := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, logPanel)

	return lipgloss.JoinVertical(lipgloss.Left, mainView, m.footerView())
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
		actionHint = m.spinner.View() + " running... "
	}

	if m.statusLine != "" {
		actionHint += m.statusLine + " · "
	}
	left = actionHint + "d:deploy r:rollback l:logs tab:switch panel q:quit"
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
