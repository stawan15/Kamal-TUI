package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────────────────────────────────────────────────────────
// Data types
// ──────────────────────────────────────────────────────────────────────────────

// ContainerStat holds one row from `docker stats --no-stream`.
type ContainerStat struct {
	Host     string // which remote server this came from
	Name     string
	CPUPct   float64
	MemUsage string
	MemLimit string
	MemPct   float64
	NetIn    string
	NetOut   string
	BlockIn  string
	BlockOut string
	StatusLv string // "ok" | "warn" | "crit"
}

// dashRefreshMsg is sent when a new poll cycle completes.
type dashRefreshMsg struct {
	stats []ContainerStat
	err   error
}

// dashTickMsg drives the periodic refresh timer.
type dashTickMsg struct{}

// ──────────────────────────────────────────────────────────────────────────────
// Dashboard-specific styles
// ──────────────────────────────────────────────────────────────────────────────

var (
	dashHdrStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			PaddingRight(1)

	dashCellStyle = lipgloss.NewStyle().
			Foreground(colorFg).
			PaddingRight(1)

	dashOkStyle = lipgloss.NewStyle().
			Foreground(colorGood).
			Bold(true).
			PaddingRight(1)

	dashWarnStyle = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true).
			PaddingRight(1)

	dashCritStyle = lipgloss.NewStyle().
			Foreground(colorBad).
			Bold(true).
			PaddingRight(1)

	dashSepStyle = lipgloss.NewStyle().
			Foreground(colorBorder)

	dashHostStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorActive).
			PaddingLeft(1)

	dashBarOk   = lipgloss.NewStyle().Foreground(colorGood)
	dashBarWarn = lipgloss.NewStyle().Foreground(colorWarning)
	dashBarCrit = lipgloss.NewStyle().Foreground(colorBad)
	dashBarBg   = lipgloss.NewStyle().Foreground(colorBorder)

	dashSummaryStyle     = lipgloss.NewStyle().Bold(true).Foreground(colorFg).Padding(0, 1)
	dashSummaryGoodStyle = lipgloss.NewStyle().Bold(true).Foreground(colorGood).Padding(0, 1)
	dashSummaryWarnStyle = lipgloss.NewStyle().Bold(true).Foreground(colorWarning).Padding(0, 1)
	dashSummaryCritStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBad).Padding(0, 1)
)

// ──────────────────────────────────────────────────────────────────────────────
// Kamal config parsing — read servers from config/deploy[.dest].yml
// ──────────────────────────────────────────────────────────────────────────────

// deployConfig mirrors the parts of Kamal's deploy.yml we care about.
type deployConfig struct {
	SSH struct {
		User string `yaml:"user"`
		Port int    `yaml:"port"`
	} `yaml:"ssh"`
	Servers interface{} `yaml:"servers"` // can be []string or map[string]role
}

type kamalRole struct {
	Hosts []string `yaml:"hosts"`
}

// readKamalHosts parses config/deploy[.dest].yml and returns all unique server hosts.
func readKamalHosts(dest string) (hosts []string, sshUser string, sshPort int) {
	candidates := []string{
		filepath.Join("config", "deploy.yml"),
	}
	if dest != "" {
		candidates = append(candidates,
			filepath.Join("config", fmt.Sprintf("deploy.%s.yml", dest)),
		)
	}

	seen := map[string]bool{}
	sshUser = "root" // Kamal default
	sshPort = 22

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg deployConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		// SSH user/port
		if cfg.SSH.User != "" {
			sshUser = cfg.SSH.User
		}
		if cfg.SSH.Port > 0 {
			sshPort = cfg.SSH.Port
		}

		// servers can be:
		//   servers:
		//     - 1.2.3.4          (simple list)
		// or
		//   servers:
		//     web:
		//       hosts: [1.2.3.4]
		//     worker:
		//       hosts: [5.6.7.8]
		extractHosts(cfg.Servers, seen)
	}

	for h := range seen {
		hosts = append(hosts, h)
	}
	return
}

func extractHosts(raw interface{}, seen map[string]bool) {
	if raw == nil {
		return
	}
	switch v := raw.(type) {
	case []interface{}:
		// Simple list of hosts
		for _, item := range v {
			if h, ok := item.(string); ok && h != "" {
				seen[h] = true
			}
		}
	case map[string]interface{}:
		for _, roleVal := range v {
			switch rv := roleVal.(type) {
			case map[string]interface{}:
				// role object: look for "hosts" key
				if hostsRaw, ok := rv["hosts"]; ok {
					extractHosts(hostsRaw, seen)
				}
			case []interface{}:
				// shorthand role: just a list
				extractHosts(rv, seen)
			}
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Polling — SSH into each remote server and run docker stats
// ──────────────────────────────────────────────────────────────────────────────

const dashPollInterval = 8 * time.Second

// mockDockerStats returns deterministic data for local UI development. It is
// intentionally independent of Docker, SSH, and Kamal configuration.
func mockDockerStats(dest string) []ContainerStat {
	hostPrefix := "dev-localhost"
	if dest != "" {
		hostPrefix = "dev-" + dest
	}
	return []ContainerStat{
		{Host: hostPrefix + "-1", Name: "kamal-tui-web-1", CPUPct: 24.6, MemUsage: "312MiB", MemLimit: "1GiB", MemPct: 30.5, NetIn: "1.2MB", NetOut: "840kB", BlockIn: "12.4MB", BlockOut: "3.1MB", StatusLv: "ok"},
		{Host: hostPrefix + "-2", Name: "kamal-tui-worker-1", CPUPct: 58.2, MemUsage: "706MiB", MemLimit: "1GiB", MemPct: 68.9, NetIn: "4.8MB", NetOut: "2.3MB", BlockIn: "41.7MB", BlockOut: "8.6MB", StatusLv: "warn"},
		{Host: hostPrefix + "-3", Name: "kamal-tui-proxy-1", CPUPct: 86.4, MemUsage: "901MiB", MemLimit: "1GiB", MemPct: 88.0, NetIn: "18.6MB", NetOut: "15.2MB", BlockIn: "92.1MB", BlockOut: "27.4MB", StatusLv: "crit"},
	}
}

// pollDockerStats fetches container stats from ALL remote Kamal servers.
// It SSHes into each host (in parallel) and runs `docker stats --no-stream`.
func pollDockerStats(ctx context.Context, dest string) ([]ContainerStat, error) {
	if devMode {
		return mockDockerStats(dest), nil
	}

	hosts, sshUser, sshPort := readKamalHosts(dest)

	// Fallback to local Docker if no Kamal config is found.
	if len(hosts) == 0 {
		return pollLocalDockerStats(ctx)
	}

	type result struct {
		stats []ContainerStat
		err   error
	}

	results := make([]result, len(hosts))
	var wg sync.WaitGroup

	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h string) {
			defer wg.Done()
			stats, err := sshDockerStats(ctx, h, sshUser, sshPort)
			results[idx] = result{stats: stats, err: err}
		}(i, host)
	}

	wg.Wait()

	var all []ContainerStat
	var firstErr error
	for _, r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		all = append(all, r.stats...)
	}

	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

// sshDockerStats runs `docker stats --no-stream` on a remote host via SSH.
func sshDockerStats(ctx context.Context, host, user string, port int) ([]ContainerStat, error) {
	target := fmt.Sprintf("%s@%s", user, host)
	portStr := strconv.Itoa(port)

	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=8",
		"-o", "BatchMode=yes",
		"-p", portStr,
		target,
		`docker stats --no-stream --format "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}"`,
	)

	out, err := cmd.Output()
	if err != nil {
		return []ContainerStat{{
			Host:     host,
			Name:     "(SSH failed)",
			StatusLv: "crit",
		}}, fmt.Errorf("ssh %s: %w", host, err)
	}

	stats := parseDockerStats(string(out))
	// Tag each stat with the host it came from
	for i := range stats {
		stats[i].Host = host
	}
	return stats, nil
}

// pollLocalDockerStats is the fallback when no config/deploy.yml is found.
func pollLocalDockerStats(ctx context.Context) ([]ContainerStat, error) {
	out, err := exec.CommandContext(ctx,
		"docker", "stats", "--no-stream",
		"--format", `{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}`,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w", err)
	}
	stats := parseDockerStats(string(out))
	for i := range stats {
		stats[i].Host = "localhost"
	}
	return stats, nil
}

func parseDockerStats(raw string) []ContainerStat {
	var stats []ContainerStat
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 6 {
			continue
		}
		cpu := parsePct(parts[1])
		mem := parsePct(parts[3])

		memParts := strings.SplitN(parts[2], " / ", 2)
		memUsage, memLimit := "", ""
		if len(memParts) == 2 {
			memUsage = strings.TrimSpace(memParts[0])
			memLimit = strings.TrimSpace(memParts[1])
		}

		netParts := strings.SplitN(parts[4], " / ", 2)
		netIn, netOut := "", ""
		if len(netParts) == 2 {
			netIn = strings.TrimSpace(netParts[0])
			netOut = strings.TrimSpace(netParts[1])
		}

		blkParts := strings.SplitN(parts[5], " / ", 2)
		blkIn, blkOut := "", ""
		if len(blkParts) == 2 {
			blkIn = strings.TrimSpace(blkParts[0])
			blkOut = strings.TrimSpace(blkParts[1])
		}

		stats = append(stats, ContainerStat{
			Name:     parts[0],
			CPUPct:   cpu,
			MemUsage: memUsage,
			MemLimit: memLimit,
			MemPct:   mem,
			NetIn:    netIn,
			NetOut:   netOut,
			BlockIn:  blkIn,
			BlockOut: blkOut,
			StatusLv: containerStatusLevel(cpu, mem),
		})
	}
	return stats
}

func parsePct(s string) float64 {
	s = strings.TrimSuffix(strings.TrimSpace(s), "%")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func containerStatusLevel(cpu, mem float64) string {
	if cpu > 80 || mem > 85 {
		return "crit"
	}
	if cpu > 50 || mem > 70 {
		return "warn"
	}
	return "ok"
}

// ──────────────────────────────────────────────────────────────────────────────
// Rendering
// ──────────────────────────────────────────────────────────────────────────────

func renderDashboard(stats []ContainerStat, lastErr error, width int, dest string) string {
	var sb strings.Builder

	// Title
	destLabel := "default"
	if dest != "" {
		destLabel = dest
	}
	sb.WriteString(titleStyle.Render(fmt.Sprintf("󰐿  Container Performance  [dest: %s]", destLabel)))
	sb.WriteString("\n\n")

	if lastErr != nil && len(stats) == 0 {
		sb.WriteString(badStyle.Render("  ✗ Error: "+lastErr.Error()) + "\n")
		sb.WriteString(helpStyle.Render("  Tip: Make sure SSH keys are set up and the server is reachable.") + "\n\n")
		sb.WriteString(helpStyle.Render("  r: retry  ·  esc: close"))
		return sb.String()
	}

	if len(stats) == 0 {
		sb.WriteString(helpStyle.Render("  No containers found on remote servers.") + "\n\n")
		sb.WriteString(helpStyle.Render("  r: retry  ·  esc: close"))
		return sb.String()
	}

	return renderDashboardCards(stats, width, dest)
}

// renderDashboardCards presents each Kamal server as a compact health card.
// Wide terminals get a two-column board; narrow terminals stay single-column
// so server boundaries remain easy to scan.
func renderDashboardCards(stats []ContainerStat, width int, dest string) string {
	var sb strings.Builder
	destLabel := "default"
	if dest != "" {
		destLabel = dest
	}
	sb.WriteString(titleStyle.Render(fmt.Sprintf("󰐿  OPERATIONS BOARD  [dest: %s]", destLabel)) + "\n")

	hostOrder := []string{}
	byHost := map[string][]ContainerStat{}
	for _, stat := range stats {
		if _, exists := byHost[stat.Host]; !exists {
			hostOrder = append(hostOrder, stat.Host)
		}
		byHost[stat.Host] = append(byHost[stat.Host], stat)
	}

	okCount, warnCount, critCount := 0, 0, 0
	for _, stat := range stats {
		switch stat.StatusLv {
		case "crit":
			critCount++
		case "warn":
			warnCount++
		default:
			okCount++
		}
	}
	summary := lipgloss.JoinHorizontal(lipgloss.Top,
		dashSummaryStyle.Render(fmt.Sprintf("%d SERVERS", len(hostOrder))),
		dashSummaryStyle.Render(fmt.Sprintf("%d CONTAINERS", len(stats))),
		dashSummaryGoodStyle.Render(fmt.Sprintf("● %d OK", okCount)),
		dashSummaryWarnStyle.Render(fmt.Sprintf("● %d WARN", warnCount)),
		dashSummaryCritStyle.Render(fmt.Sprintf("● %d CRIT", critCount)),
	)
	sb.WriteString(summary + "\n\n")

	available := maxInt(width-8, 30)
	cols := 1
	if available >= 96 {
		cols = 2
	}
	cardGap := 2
	cardWidth := available
	if cols == 2 {
		cardWidth = (available - cardGap) / cols
	}

	cards := make([]string, 0, len(hostOrder))
	for _, host := range hostOrder {
		cards = append(cards, renderHostCard(host, byHost[host], cardWidth))
	}
	for i := 0; i < len(cards); i += cols {
		row := cards[i]
		for j := 1; j < cols && i+j < len(cards); j++ {
			row = lipgloss.JoinHorizontal(lipgloss.Top, row, strings.Repeat(" ", cardGap), cards[i+j])
		}
		sb.WriteString(row + "\n\n")
	}

	sep := dashSepStyle.Render(strings.Repeat("─", minInt(width-6, 118)))
	sb.WriteString(sep + "\n")
	sb.WriteString(helpStyle.Render(fmt.Sprintf(
		"  REFRESHED %s  ·  EVERY %ds  ·  r: refresh now  ·  esc: close",
		time.Now().Format("15:04:05"), int(dashPollInterval.Seconds()),
	)))
	return sb.String()
}

func renderHostCard(host string, stats []ContainerStat, width int) string {
	worst := "ok"
	for _, stat := range stats {
		if stat.StatusLv == "crit" {
			worst = "crit"
			break
		}
		if stat.StatusLv == "warn" {
			worst = "warn"
		}
	}

	status := dashOkStyle.Render("● HEALTHY")
	if worst == "warn" {
		status = dashWarnStyle.Render("● DEGRADED")
	} else if worst == "crit" {
		status = dashCritStyle.Render("● CRITICAL")
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		dashHostStyle.Render("󰒍  "+trunc(host, maxInt(width-20, 8))),
		status,
	)

	rows := []string{header, dashSepStyle.Render(strings.Repeat("─", maxInt(width-4, 10)))}
	for _, stat := range stats {
		indicator := dashOkStyle.Render("●")
		if stat.StatusLv == "warn" {
			indicator = dashWarnStyle.Render("●")
		} else if stat.StatusLv == "crit" {
			indicator = dashCritStyle.Render("●")
		}
		nameWidth := maxInt(width-38, 10)
		name := dashCellStyle.Render(trunc(stat.Name, nameWidth))
		metrics := fmt.Sprintf("CPU %5.1f%%  MEM %5.1f%%", stat.CPUPct, stat.MemPct)
		metricWidth := maxInt(width-nameWidth-4, 12)
		rows = append(rows, indicator+" "+name+" "+colorizePct(metrics, stat.CPUPct, 50, 80, metricWidth))
		rows = append(rows, "  "+miniBar(stat.CPUPct, maxInt(width/3, 8), stat.StatusLv)+"  "+miniBar(stat.MemPct, maxInt(width/3, 8), stat.StatusLv))
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Background(colorPanelBg).
		Padding(0, 1).
		Width(width)
	return cardStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func colorizePct(s string, val, warnT, critT float64, w int) string {
	switch {
	case val >= critT:
		return dashCritStyle.Width(w).Render(trunc(s, w-1))
	case val >= warnT:
		return dashWarnStyle.Width(w).Render(trunc(s, w-1))
	default:
		return dashOkStyle.Width(w).Render(trunc(s, w-1))
	}
}

func miniBar(pct float64, barW int, status string) string {
	filled := int(pct / 100.0 * float64(barW))
	if filled > barW {
		filled = barW
	}
	if filled < 0 {
		filled = 0
	}
	empty := barW - filled

	var barStyle *lipgloss.Style
	switch status {
	case "crit":
		barStyle = &dashBarCrit
	case "warn":
		barStyle = &dashBarWarn
	default:
		barStyle = &dashBarOk
	}

	filledStr := barStyle.Render(strings.Repeat("█", filled))
	emptyStr := dashBarBg.Render(strings.Repeat("░", empty))
	return fmt.Sprintf("[%s%s] %4.1f%%", filledStr, emptyStr, pct)
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
