package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var devMode bool

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

type logLineMsg struct {
	runID uint64
	line  string
}
type logStreamClosedMsg struct{ runID uint64 }
type cmdDoneMsg struct {
	runID uint64
	err   error
}
type uiAnimationTickMsg struct{}

const uiAnimationInterval = 180 * time.Millisecond

var uiAnimationGlyphs = []string{"✦", "✧", "⋆", "✧"}

const mascotArt = `             __
        _.-'  '-._
     .-'  _    _  '-.
    /   _/ \__/ \_   \__
   /   /  _      _ \     \
  |   |  (o)    (o) |      )
  |    \     __     /    _/
   \    '-._/  \_.-'   _/
    '-._           _.-'
        \__ /\ __/
       /  /  \  \
      /__/    \__\`

var logHighlightPattern = regexp.MustCompile(`(?i)\b(?:ERROR|FATAL|PANIC|WARN(?:ING)?|INFO|DEBUG|TRACE)\b|\b[1-5][0-9]{2}\b|https?://[^\s]+|\b(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+[^\s]+|\b(?:success(?:ful)?|failed|failure|started|completed)\b`)
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

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

	lineCh    chan string
	doneCh    chan error
	cancel    context.CancelFunc
	runID     uint64
	cancelled bool

	outputBuf []string

	logFilter     string
	logFilterIn   textinput.Model
	showLogFilter bool
	logFollowing  bool

	// Log server paging. An empty host means all servers.
	logHosts     []string
	logHostIndex int

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
	showDashboard bool
	dashStats     []ContainerStat
	dashErr       error
	dashLoading   bool

	// Visual-only animation state. It never affects command behavior.
	animFrame int
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
	if devMode {
		dests = []string{"production", "staging", "preview"}
	}
	ditems := make([]list.Item, 0, len(dests))
	for _, d := range dests {
		ditems = append(ditems, destItem(d))
	}
	dl := list.New(ditems, list.NewDefaultDelegate(), 0, 0)
	dl.Title = ""
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
	secList.Title = ""
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

	logFilterIn := textinput.New()
	logFilterIn.Prompt = "/ "
	logFilterIn.Placeholder = "grep logs..."
	logFilterIn.CharLimit = 128

	welcomeView := mascotStyle.Render(mascotArt) +
		"\n\n" +
		sectionLabelStyle.Render("KAMAL // OPS DECK") +
		"\n" +
		helpStyle.Render("Select a target, then choose an action to begin.") +
		"\n" +
		helpStyle.Render("Press x for the command menu · p for the operations board")
	vp.SetContent(welcomeView)
	vp.GotoTop()

	return model{
		activePanel:  panelDestinations,
		destList:     dl,
		verInput:     ti,
		viewport:     vp,
		spinner:      sp,
		outputBuf:    []string{welcomeView},
		logFilterIn:  logFilterIn,
		logFollowing: true,
		secList:      secList,
		secKeyIn:     secKeyIn,
		secValIn:     secValIn,
		projectName:  detectProjectName(),
		gitBranch:    detectGitBranch(),
	}
}

func (m model) Init() tea.Cmd {
	return uiAnimationTick()
}

func uiAnimationTick() tea.Cmd {
	return tea.Tick(uiAnimationInterval, func(time.Time) tea.Msg {
		return uiAnimationTickMsg{}
	})
}

func (m model) animationGlyph() string {
	return uiAnimationGlyphs[m.animFrame%len(uiAnimationGlyphs)]
}

// highlightLogLine adds semantic color to common log tokens while preserving
// the original text. It intentionally stays framework-agnostic.
func highlightLogLine(line string) string {
	return logHighlightPattern.ReplaceAllStringFunc(line, func(token string) string {
		lower := strings.ToLower(token)
		switch {
		case strings.Contains(lower, "error"), strings.Contains(lower, "fatal"), strings.Contains(lower, "panic"), strings.Contains(lower, "failed"), strings.Contains(lower, "failure"):
			return logErrorStyle.Render(token)
		case strings.Contains(lower, "warn"):
			return logWarnStyle.Render(token)
		case strings.Contains(lower, "success"), strings.Contains(lower, "started"), strings.Contains(lower, "completed"):
			return logSuccessStyle.Render(token)
		case lower == "info", lower == "debug", lower == "trace":
			return logInfoStyle.Render(token)
		case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
			return logURLStyle.Render(token)
		case strings.HasPrefix(lower, "get "), strings.HasPrefix(lower, "post "), strings.HasPrefix(lower, "put "), strings.HasPrefix(lower, "patch "), strings.HasPrefix(lower, "delete "), strings.HasPrefix(lower, "head "), strings.HasPrefix(lower, "options "):
			return logHTTPStyle.Render(token)
		default:
			return logStatusStyle.Render(token)
		}
	})
}

func (m model) activeStyle() lipgloss.Style {
	// Alternate between the accent colors to create a restrained pulse around
	// the focused panel without changing layout or interaction behavior.
	border := colorActive
	if m.animFrame%6 >= 3 {
		border = colorAccent
	}
	return activePanelStyle.BorderForeground(border)
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

func waitForLine(ch <-chan string, runID uint64) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return logStreamClosedMsg{runID: runID}
		}
		return logLineMsg{runID: runID, line: line}
	}
}

func waitForDone(ch <-chan error, runID uint64) tea.Cmd {
	return func() tea.Msg {
		err := <-ch
		return cmdDoneMsg{runID: runID, err: err}
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
	listH := bodyH - 7
	if listH < 3 {
		listH = 3
	}
	m.destList.SetSize(leftW-4, listH)

	m.viewport.Width = rightW - 4
	logH := bodyH - 8
	if logH < 3 {
		logH = 3
	}
	m.viewport.Height = logH

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

	if action.key == "l" {
		m.setLogHosts(dest)
	}

	return m.promptConfirm(action, dest, "")
}

// setLogHosts discovers hosts for the selected Kamal destination. The first
// entry is always the aggregate view; subsequent entries are individual hosts.
func (m *model) setLogHosts(dest string) {
	if devMode {
		m.logHosts = []string{"", "web-1", "web-2", "worker-1", "worker-2"}
		m.logHostIndex = 0
		return
	}
	hosts, _, _ := readKamalHosts(dest)
	m.logHosts = append([]string{""}, hosts...)
	m.logHostIndex = 0
}

func (m model) selectedLogHost() string {
	if m.logHostIndex <= 0 || m.logHostIndex >= len(m.logHosts) {
		return ""
	}
	return m.logHosts[m.logHostIndex]
}

func (m model) logHostLabel() string {
	host := m.selectedLogHost()
	if host == "" {
		return "all servers"
	}
	return host
}

func (m model) logArgs(action actionItem, dest, version string) []string {
	args := action.buildArgs(dest, version)
	if action.key == "l" {
		args = append(args, "-f")
		if host := m.selectedLogHost(); host != "" {
			args = append(args, "--hosts", host)
		}
		if m.logFilter != "" {
			args = append(args, "--grep", m.logFilter)
		}
	}
	return args
}

func (m *model) openLogFilter() tea.Cmd {
	m.showLogFilter = true
	m.logFilterIn.SetValue(m.logFilter)
	m.logFilterIn.Focus()
	return textinput.Blink
}

func (m model) applyLogFilter() (tea.Model, tea.Cmd) {
	m.logFilter = strings.TrimSpace(m.logFilterIn.Value())
	m.showLogFilter = false
	m.logFilterIn.Blur()
	if m.selectedAction.key != "l" {
		return m, nil
	}
	if m.cancel != nil {
		m.cancel()
	}
	dest := ""
	if it, ok := m.destList.SelectedItem().(destItem); ok {
		dest = string(it)
	}
	return m.startRun(m.selectedAction, dest, "")
}

func (m model) copyLogs() (tea.Model, tea.Cmd) {
	if len(m.outputBuf) == 0 {
		return m, nil
	}
	raw := ansiEscapePattern.ReplaceAllString(strings.Join(m.outputBuf, "\n"), "")
	if err := clipboard.WriteAll(raw); err != nil {
		m.statusLine = badStyle.Render("copy failed: " + err.Error())
	} else {
		m.statusLine = okStyle.Render("log copied")
	}
	return m, nil
}

func (m model) switchLogHost(delta int) (tea.Model, tea.Cmd) {
	if m.showConfirm || m.showMenu || m.showSecrets || m.addingSecret || m.showVersionInput || m.showDashboard || m.selectedAction.key != "l" || len(m.logHosts) < 2 {
		return m, nil
	}
	m.logHostIndex = (m.logHostIndex + delta + len(m.logHosts)) % len(m.logHosts)
	if m.running && m.cancel != nil {
		m.cancel()
	}
	dest := ""
	if it, ok := m.destList.SelectedItem().(destItem); ok {
		dest = string(it)
	}
	return m.startRun(m.selectedAction, dest, "")
}

func (m model) promptConfirm(action actionItem, dest, version string) (tea.Model, tea.Cmd) {
	m.showConfirm = true
	m.confirmAct = action
	m.confirmDest = dest
	m.confirmVer = version
	args := m.logArgs(action, dest, version)
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

	case uiAnimationTickMsg:
		m.animFrame = (m.animFrame + 1) % len(uiAnimationGlyphs)
		return m, uiAnimationTick()

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
		if m.showLogFilter {
			switch msg.String() {
			case "enter":
				return m.applyLogFilter()
			case "esc":
				m.showLogFilter = false
				m.logFilterIn.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.logFilterIn, cmd = m.logFilterIn.Update(msg)
				return m, cmd
			}
		}
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
			if m.running {
				m.cancelled = true
				m.running = false
				m.statusLine = okStyle.Render("cancelled")
				if m.cancel != nil {
					m.cancel()
				}
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
			if m.showLogFilter {
				m.showLogFilter = false
				m.logFilterIn.Blur()
				return m, nil
			}
			if m.running {
				m.cancelled = true
				m.running = false
				m.statusLine = okStyle.Render("cancelled")
				if m.cancel != nil {
					m.cancel()
				}
				return m, nil
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
		case "[":
			return m.switchLogHost(-1)
		case "]":
			return m.switchLogHost(1)
		case "/":
			if m.selectedAction.key == "l" && !m.showConfirm && !m.showMenu && !m.showDashboard {
				return m, m.openLogFilter()
			}
		case "c":
			if m.selectedAction.key == "l" && !m.showConfirm && !m.showMenu && !m.showDashboard {
				return m.copyLogs()
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
				if m.selectedAction.key == "l" {
					switch msg.String() {
					case "up", "pageup", "ctrl+u", "home":
						m.logFollowing = false
					case "end":
						m.logFollowing = true
					}
				}
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
			}
		}

	case logLineMsg:
		if msg.runID != m.runID {
			return m, nil
		}
		line := msg.line
		if m.logFilter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(m.logFilter)) {
			return m, waitForLine(m.lineCh, m.runID)
		}
		m.outputBuf = append(m.outputBuf, highlightLogLine(line))
		m.viewport.SetContent(strings.Join(m.outputBuf, "\n"))
		if m.logFollowing {
			m.viewport.GotoBottom()
		}
		return m, waitForLine(m.lineCh, m.runID)

	case logStreamClosedMsg:
		if msg.runID != m.runID {
			return m, nil
		}
		return m, nil

	case cmdDoneMsg:
		if msg.runID != m.runID {
			return m, nil
		}
		m.running = false
		m.lastErr = msg.err
		if m.cancelled {
			m.statusLine = okStyle.Render("cancelled")
		} else if msg.err != nil {
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
	m.runID++
	m.cancelled = false
	m.selectedAction = action
	m.outputBuf = nil
	m.logFollowing = true
	m.statusLine = ""
	m.lastErr = nil
	m.verInput.Blur()

	m.verInput.SetValue("")
	args := m.logArgs(action, dest, version)

	m.outputBuf = []string{highlightLogLine("$ kamal " + strings.Join(args, " "))}

	go runKamal(ctx, dest, nil, args, m.lineCh, m.doneCh)

	m.viewport.SetContent(strings.Join(m.outputBuf, "\n"))
	return m, tea.Batch(m.spinner.Tick, waitForLine(m.lineCh, m.runID), waitForDone(m.doneCh, m.runID))
}

// headerView renders the animated brand on the left and project::branch on the right.
func (m model) headerView() string {
	var label string
	if m.gitBranch != "" {
		label = m.projectName + " :: " + m.gitBranch
	} else {
		label = m.projectName
	}
	brand := brandStyle.Render(m.animationGlyph() + "  KAMAL TUI")
	state := "READY"
	if m.running {
		state = "● LIVE STREAM"
	}
	if devMode {
		state = "◇ DEV MODE"
	}
	status := badgeStyle.Render(state)
	right := headerBranchStyle.Render(label)
	padding := m.width - lipgloss.Width(brand) - lipgloss.Width(status) - lipgloss.Width(right)
	if padding < 1 {
		padding = 1
	}
	return lipgloss.NewStyle().Background(colorHeaderBg).Width(m.width).Render(
		brand + status + strings.Repeat(" ", padding) + right,
	)
}

func (m model) destinationPanelContent() string {
	count := len(m.destList.Items())
	heading := lipgloss.JoinHorizontal(lipgloss.Top,
		sectionLabelStyle.Render("◈ TARGETS"),
		badgeStyle.Render(fmt.Sprintf("%02d", count)),
	)
	meta := headerMetaStyle.Render("KAMAL DESTINATIONS  /  SELECT TARGET")
	hint := helpStyle.Render("↑↓ navigate  ·  enter focus logs")
	return lipgloss.JoinVertical(lipgloss.Left, heading, meta, m.destList.View(), hint)
}

func (m model) logPanelContent(logContent string) string {
	target := "ALL SERVERS"
	if m.selectedAction.key == "l" {
		target = strings.ToUpper(m.logHostLabel())
	}
	heading := lipgloss.JoinHorizontal(lipgloss.Top,
		sectionLabelStyle.Render("▣ ACTIVITY STREAM"),
		badgeStyle.Render(target),
	)
	meta := headerMetaStyle.Render("REAL-TIME COMMAND OUTPUT  /  " + strings.ToUpper(m.projectName))
	filter := helpStyle.Render("COMMAND OUTPUT")
	if m.selectedAction.key == "l" {
		filter = helpStyle.Render("/ grep  ·  c copy  ·  ↑↓ scroll  ·  End live tail")
		if m.logFilter != "" {
			filter = logFilterStyle.Render("grep: "+m.logFilter) + "  " + filter
		}
		if !m.logFollowing {
			filter += "  " + logPausedStyle.Render("PAUSED")
		}
		if m.showLogFilter {
			filter = m.logFilterIn.View() + "  " + helpStyle.Render("enter apply · esc cancel")
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, heading, meta, filter, logContent)
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
			titleStyle.Render("KAMAL // OPS DECK"),
			"",
			mascotStyle.Render(mascotArt),
			mascotCaptionStyle.Render("THE DEPLOY CAMEL"),
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
		return m.activeStyle().
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
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.activeStyle().Width(50).Render(content))
	}
	if m.showSecrets {
		content := lipgloss.JoinVertical(lipgloss.Left,
			m.secList.View(),
			"",
			helpStyle.Render("a: add secret · x/d: delete · esc: back"),
		)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.activeStyle().Width(m.width-6).Height(m.height-2).Render(content))
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
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.activeStyle().Width(m.width-10).Render(content))
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
		style = m.activeStyle()
	}
	destPanel := style.Width(leftW - 2).Height(bodyH - 2).Render(m.destinationPanelContent())

	// Render Logs panel
	style = inactivePanelStyle
	if m.activePanel == panelLogs {
		style = m.activeStyle()
	}

	logContent := m.viewport.View()
	if m.showVersionInput {
		overlay := lipgloss.JoinVertical(lipgloss.Left,
			titleStyle.Render("Rollback version:"),
			"",
			m.verInput.View(),
		)
		logContent = lipgloss.Place(rightW-4, bodyH-4, lipgloss.Center, lipgloss.Center, m.activeStyle().Render(overlay))
	}

	logInner := m.logPanelContent(logContent)
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
	actionHint := ""
	if m.running {
		actionHint = m.spinner.View() + " RUNNING  "
	}

	if m.statusLine != "" {
		actionHint += m.statusLine + "  "
	}
	keys := []string{
		keyCapStyle.Render("d") + " deploy",
		keyCapStyle.Render("p") + " dashboard",
		keyCapStyle.Render("x") + " menu",
		keyCapStyle.Render("s") + " secrets",
		keyCapStyle.Render("q") + " quit",
	}
	if m.selectedAction.key == "l" && len(m.logHosts) > 1 {
		keys = append(keys, keyCapStyle.Render("[ ]")+" servers")
	}
	dock := strings.Join(keys, "   ")
	if actionHint != "" || m.statusLine != "" {
		dock = actionHint + "   │   " + dock
	}
	return statusBarStyle.Width(m.width).Render(dock)
}

func main() {
	devFlag := flag.Bool("dev", false, "use mock dashboard data and command output without Docker, SSH, or Kamal")
	flag.Parse()
	devMode = *devFlag || os.Getenv("KAMAL_TUI_DEV") == "1" || os.Getenv("KAMAL_TUI_DEV") == "true"

	if !devMode {
		if _, _, ok := kamalBinaryAvailable(); !ok {
			fmt.Fprintln(os.Stderr, "warning: kamal binary not found yet (checked PATH, bin/kamal, bundle exec). Run this from your Rails project root, or install kamal first.")
		}
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
