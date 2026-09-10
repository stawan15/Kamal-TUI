package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

// actionItem describes one action available in the main menu.
type actionItem struct {
	key          string
	title        string
	desc         string
	needsVersion bool // rollback needs a version/commit hash typed in
	buildArgs    func(dest, version string) []string
}

func (a actionItem) Title() string       { return a.title }
func (a actionItem) Description() string { return a.desc }
func (a actionItem) FilterValue() string { return a.title }

func actions() []actionItem {
	return []actionItem{
		{
			key:   "d",
			title: "󰚰 Deploy",
			desc:  "kamal deploy -d <destination>",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"deploy"}, dest)
			},
		},
		{
			key:   "U",
			title: "󰒓 Setup",
			desc:  "kamal setup -d <destination> (provision servers & deploy)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"setup"}, dest)
			},
		},
		{
			key:   "R",
			title: "󰑙 Redeploy",
			desc:  "kamal redeploy -d <destination>",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"redeploy"}, dest)
			},
		},
		{
			key:          "r",
			title:        "󰁯 Rollback",
			desc:         "kamal rollback <version> -d <destination>",
			needsVersion: true,
			buildArgs: func(dest, version string) []string {
				args := []string{"rollback", version}
				return withDest(args, dest)
			},
		},
		{
			key:   "e",
			title: "󰈙 Env Push",
			desc:  "kamal env push -d <destination> (push .env variables to servers)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"env", "push"}, dest)
			},
		},
		{
			key:   "A",
			title: "󰋩 App Details",
			desc:  "kamal app details -d <destination>",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"app", "details"}, dest)
			},
		},
		{
			key:   "l",
			title: "󰅩 App Logs",
			desc:  "kamal app logs -f -d <destination> (live tail)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"app", "logs"}, dest)
			},
		},
		{
			key:   "b",
			title: "󰀵 App Boot",
			desc:  "kamal app boot -d <destination>",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"app", "boot"}, dest)
			},
		},
		{
			key:   "D",
			title: "󰆼 DB Dump (Backup)",
			desc:  "kamal app exec -i -- /bin/sh -c 'pg_dump ...'",
			buildArgs: func(dest, _ string) []string {
				args := []string{"app", "exec", "-i"}
				args = withDest(args, dest)
				args = append(args, "--", "/bin/sh", "-c", "pg_dump $DATABASE_URL -F c > /tmp/db.dump")
				return args
			},
		},
		{
			key:   "S",
			title: "󰗨 DB Restore",
			desc:  "kamal app exec -i -- /bin/sh -c 'pg_restore ...'",
			buildArgs: func(dest, _ string) []string {
				args := []string{"app", "exec", "-i"}
				args = withDest(args, dest)
				args = append(args, "--", "/bin/sh", "-c", "pg_restore -d $DATABASE_URL --clean --no-owner /tmp/db.dump")
				return args
			},
		},
		{
			key:   "a",
			title: "󰚌 Audit",
			desc:  "kamal audit -d <destination> (recent deploy history)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"audit"}, dest)
			},
		},
		{
			key:   "X",
			title: "󰆴 Remove",
			desc:  "kamal remove -d <destination> (remove containers and images from servers)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"remove"}, dest)
			},
		},
	}
}

// actionByKey finds an action by its shortcut key.
func actionByKey(key string) (actionItem, bool) {
	for _, a := range actions() {
		if a.key == key {
			return a, true
		}
	}
	return actionItem{}, false
}

// withDest appends "-d <dest>" unless dest is the empty/default destination.
func withDest(args []string, dest string) []string {
	if dest != "" {
		args = append(args, "-d", dest)
	}
	return args
}

// discoverDestinations scans ./config for deploy.<name>.yml files (Kamal's
// multi-destination convention) and returns the destination names it finds.
// If only config/deploy.yml exists, it returns a single empty-string entry
// meaning "default / no -d flag".
func discoverDestinations() []string {
	entries, err := os.ReadDir("config")
	if err != nil {
		// config/ not found (wrong directory) — still offer the default
		// destination so the user gets a helpful kamal error rather than
		// an empty, unusable list.
		return []string{""}
	}
	var dests []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "deploy.yml" {
			continue
		}
		if strings.HasPrefix(name, "deploy.") && strings.HasSuffix(name, ".yml") {
			dest := strings.TrimSuffix(strings.TrimPrefix(name, "deploy."), ".yml")
			if dest != "" {
				dests = append(dests, dest)
			}
		}
	}
	sort.Strings(dests)
	// Always include the default (no -d flag) option, even alongside named
	// destinations — some setups still deploy config/deploy.yml directly.
	dests = append(dests, "")
	return dests
}

// kamalBinaryAvailable checks whether the kamal executable can be found,
// either on PATH or via `bundle exec kamal` (common when kamal is a gem
// dependency rather than a global install).
func kamalBinaryAvailable() (bin string, args []string, ok bool) {
	if p, err := exec.LookPath("kamal"); err == nil {
		return p, nil, true
	}
	if _, err := os.Stat(filepath.Join("bin", "kamal")); err == nil {
		return "bin/kamal", nil, true
	}
	if _, err := exec.LookPath("bundle"); err == nil {
		if _, err := os.Stat("Gemfile"); err == nil {
			return "bundle", []string{"exec", "kamal"}, true
		}
	}
	return "", nil, false
}

// runKamal starts the kamal command in the background, streaming combined
// stdout+stderr lines to lineCh (closed when the process finishes producing
// output) and sending the final error (nil on success) to doneCh exactly
// once. Cancel ctx to kill the process early.
func runKamal(ctx context.Context, dest string, prefixArgs []string, args []string, lineCh chan<- string, doneCh chan<- error) {
	if devMode {
		runMockKamal(ctx, args, lineCh, doneCh)
		return
	}

	full := append(append([]string{}, prefixArgs...), args...)
	bin, extra, ok := kamalBinaryAvailable()
	if !ok {
		doneCh <- errBinaryNotFound
		close(lineCh)
		return
	}
	full = append(extra, full...)

	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Env = loadEnvForDest(dest)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
	}()

	go func() {
		startErr := cmd.Start()
		if startErr != nil {
			pw.Close()
			doneCh <- startErr
			return
		}
		waitErr := cmd.Wait()
		pw.Close()
		doneCh <- waitErr
	}()
}

// runMockKamal exercises the command output UI without invoking Kamal. It is
// used by --dev for safely testing deploy, rollback, and log interactions.
func runMockKamal(ctx context.Context, args []string, lineCh chan<- string, doneCh chan<- error) {
	defer close(lineCh)

	host := "all servers"
	for i, arg := range args {
		if arg == "--hosts" && i+1 < len(args) {
			host = args[i+1]
		}
	}

	lines := []string{
		"[dev] mock Kamal command: kamal " + strings.Join(args, " "),
		"[dev] target: " + host,
		"[dev] connecting to mock server...",
	}
	if len(args) >= 2 && args[0] == "app" && args[1] == "logs" {
		lines = append(lines, mockLogLines(host)...)
	} else {
		lines = append(lines,
			"[dev] pulling image kamal-tui:demo",
			"[dev] starting container web-1",
			"[dev] command completed successfully",
		)
	}

	for _, line := range lines {
		select {
		case lineCh <- line:
		case <-ctx.Done():
			doneCh <- ctx.Err()
			return
		}
	}
	doneCh <- nil
}

// mockLogLines is intentionally noisy and varied so developers can see the
// log highlighting, viewport scrolling, and multi-server paging in --dev.
func mockLogLines(host string) []string {
	return []string{
		"2026-09-09T21:14:02Z INFO  " + host + " deploy started version=2.1.0",
		"2026-09-09T21:14:03Z DEBUG GET /health 200 12ms",
		"2026-09-09T21:14:04Z INFO  GET /api/projects 200 48ms",
		"2026-09-09T21:14:05Z INFO  POST /api/deployments 201 182ms",
		"2026-09-09T21:14:06Z WARN  queue depth=74 latency=420ms",
		"2026-09-09T21:14:07Z DEBUG worker processed job=sync-1842",
		"2026-09-09T21:14:08Z INFO  GET https://registry.example.com/v2/health 200",
		"2026-09-09T21:14:09Z ERROR GET /api/health 503 upstream timeout",
		"2026-09-09T21:14:10Z WARN  retrying request attempt=2 backoff=500ms",
		"2026-09-09T21:14:11Z INFO  GET /api/health 200 recovered=true",
		"2026-09-09T21:14:12Z DEBUG cache miss key=deploy:latest",
		"2026-09-09T21:14:13Z INFO  completed migration AddDeployments",
		"2026-09-09T21:14:14Z INFO  successful release health check",
		"2026-09-09T21:14:15Z ERROR database connection refused host=db.internal",
		"2026-09-09T21:14:16Z WARN  reconnecting to postgres in 2s",
		"2026-09-09T21:14:18Z INFO  database connection restored",
		"2026-09-09T21:14:19Z TRACE DELETE /tmp/releases/old 204",
		"2026-09-09T21:14:20Z INFO  GET / 200 6ms user=demo",
		"2026-09-09T21:14:21Z PANIC worker crashed: simulated demo failure",
		"2026-09-09T21:14:22Z INFO  process restarted successfully",
	}
}

// loadEnvForDest reads secrets from standard kamal env locations
func loadEnvForDest(dest string) []string {
	envFiles := []string{
		".env",
		filepath.Join(".kamal", "secrets-common"),
		filepath.Join("kamal", "secrets-common"),
		filepath.Join(".kamal", "secrets"),
		filepath.Join("kamal", "secrets"),
	}
	if dest != "" {
		envFiles = append(envFiles,
			fmt.Sprintf(".env.%s", dest),
			filepath.Join(".kamal", fmt.Sprintf("secrets-%s", dest)),
			filepath.Join("kamal", fmt.Sprintf("secrets-%s", dest)),
		)
	}

	envMap := make(map[string]string)
	for _, f := range envFiles {
		if m, err := godotenv.Read(f); err == nil {
			for k, v := range m {
				envMap[k] = v
			}
		}
	}

	// Load secrets from keychain last so they intentionally override file values.
	keychainSecrets := loadSecrets()
	for k, v := range keychainSecrets {
		envMap[k] = v
	}

	cmdEnv := os.Environ()
	for k, v := range envMap {
		found := false
		prefix := k + "="
		for i, e := range cmdEnv {
			if strings.HasPrefix(e, prefix) {
				cmdEnv[i] = prefix + v
				found = true
				break
			}
		}
		if !found {
			cmdEnv = append(cmdEnv, prefix+v)
		}
	}
	return cmdEnv
}
