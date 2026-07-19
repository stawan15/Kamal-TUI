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

// pollDockerStats fetches container stats from ALL remote Kamal servers.
// It SSHes into each host (in parallel) and runs `docker stats --no-stream`.
func pollDockerStats(ctx context.Context, dest string) ([]ContainerStat, error) {
	hosts, sshUser, sshPort := readKamalHosts(dest)

	// Fallback to local docker if no config found (dev mode)
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
	const (
		colName   = 30
		colCPU    = 9
		colMem    = 22
		colMemPct = 9
		colNet    = 22
		colBlk    = 20
	)

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

	sep := dashSepStyle.Render(strings.Repeat("─", minInt(width-6, 118)))

	// Column header
	hdr := dashHdrStyle.Width(colName).Render(trunc("CONTAINER", colName-1)) +
		dashHdrStyle.Width(colCPU).Render("CPU%") +
		dashHdrStyle.Width(colMem).Render("MEM USAGE/LIMIT") +
		dashHdrStyle.Width(colMemPct).Render("MEM%") +
		dashHdrStyle.Width(colNet).Render("NET IN/OUT") +
		dashHdrStyle.Width(colBlk).Render("BLK IN/OUT")

	// Group stats by host
	hostOrder := []string{}
	byHost := map[string][]ContainerStat{}
	for _, s := range stats {
		if _, exists := byHost[s.Host]; !exists {
			hostOrder = append(hostOrder, s.Host)
		}
		byHost[s.Host] = append(byHost[s.Host], s)
	}

	for _, host := range hostOrder {
		hostStats := byHost[host]

		// Server section header
		sb.WriteString(dashHostStyle.Render(fmt.Sprintf("󰒍  %s", host)) + "\n")
		sb.WriteString("  " + hdr + "\n")
		sb.WriteString("  " + sep + "\n")

		for _, s := range hostStats {
			cpuStr := fmt.Sprintf("%.1f%%", s.CPUPct)
			memStr := fmt.Sprintf("%s/%s", s.MemUsage, s.MemLimit)
			memPctStr := fmt.Sprintf("%.1f%%", s.MemPct)
			netStr := fmt.Sprintf("%s/%s", s.NetIn, s.NetOut)
			blkStr := fmt.Sprintf("%s/%s", s.BlockIn, s.BlockOut)

			indicator := dashOkStyle.Render("●")
			switch s.StatusLv {
			case "warn":
				indicator = dashWarnStyle.Render("●")
			case "crit":
				indicator = dashCritStyle.Render("●")
			}

			row := "  " + indicator + " " +
				dashCellStyle.Width(colName-3).Render(trunc(s.Name, colName-4)) +
				colorizePct(cpuStr, s.CPUPct, 50, 80, colCPU) +
				dashCellStyle.Width(colMem).Render(trunc(memStr, colMem-1)) +
				colorizePct(memPctStr, s.MemPct, 70, 85, colMemPct) +
				dashCellStyle.Width(colNet).Render(trunc(netStr, colNet-1)) +
				dashCellStyle.Width(colBlk).Render(trunc(blkStr, colBlk-1))

			sb.WriteString(row + "\n")

			// Mini bars
			cpuBar := miniBar(s.CPUPct, 30, s.StatusLv)
			memBar := miniBar(s.MemPct, 30, s.StatusLv)
			sb.WriteString(fmt.Sprintf("     %s CPU   %s MEM\n", cpuBar, memBar))
			sb.WriteString("\n")
		}
	}

	// Footer
	sb.WriteString("  " + sep + "\n")
	ts := time.Now().Format("15:04:05")
	sb.WriteString(helpStyle.Render(fmt.Sprintf(
		"  Refreshed: %s  ·  Every %ds  ·  r: refresh now  ·  esc: close",
		ts, int(dashPollInterval.Seconds()),
	)))

	return sb.String()
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
