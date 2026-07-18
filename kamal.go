package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// actionItem describes one action available in the main menu.
type actionItem struct {
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
			title: "🚀 Deploy",
			desc:  "kamal deploy -d <destination>",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"deploy"}, dest)
			},
		},
		{
<<<<<<< Updated upstream
			title: "Redeploy",
=======
			title: "⚙️  Setup",
			desc:  "kamal setup -d <destination> (provision servers & deploy)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"setup"}, dest)
			},
		},
		{
			title: "🔑 Env Push",
			desc:  "kamal env push -d <destination> (push .env variables to servers)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"env", "push"}, dest)
			},
		},
		{
			title: "♻️  Redeploy",
>>>>>>> Stashed changes
			desc:  "kamal redeploy -d <destination> (skip build cache invalidation steps)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"redeploy"}, dest)
			},
		},
		{
			title:        "⏪ Rollback",
			desc:         "kamal rollback <version> -d <destination>",
			needsVersion: true,
			buildArgs: func(dest, version string) []string {
				args := []string{"rollback", version}
				return withDest(args, dest)
			},
		},
		{
			title: "💾 DB Dump (Backup)",
			desc:  "kamal app exec -i -- /bin/sh -c 'pg_dump ...'",
			buildArgs: func(dest, _ string) []string {
				args := []string{"app", "exec", "-i"}
				args = withDest(args, dest)
				args = append(args, "--", "/bin/sh", "-c", "pg_dump $DATABASE_URL -F c > /tmp/db.dump")
				return args
			},
		},
		{
			title: "💿 DB Restore",
			desc:  "kamal app exec -i -- /bin/sh -c 'pg_restore ...'",
			buildArgs: func(dest, _ string) []string {
				args := []string{"app", "exec", "-i"}
				args = withDest(args, dest)
				args = append(args, "--", "/bin/sh", "-c", "pg_restore -d $DATABASE_URL --clean --no-owner /tmp/db.dump")
				return args
			},
		},
		{
			title: "ℹ️  App Details",
			desc:  "kamal app details -d <destination>",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"app", "details"}, dest)
			},
		},
		{
			title: "📝 App Logs",
			desc:  "kamal app logs -d <destination> (last lines, no follow)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"app", "logs"}, dest)
			},
		},
		{
			title: "⚡ App Boot",
			desc:  "kamal app boot -d <destination>",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"app", "boot"}, dest)
			},
		},
		{
			title: "🕒 Audit",
			desc:  "kamal audit -d <destination> (recent deploy history)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"audit"}, dest)
			},
		},
<<<<<<< Updated upstream
=======
		{
			title: "🗑️  Remove",
			desc:  "kamal remove -d <destination> (remove containers and images from servers)",
			buildArgs: func(dest, _ string) []string {
				return withDest([]string{"remove"}, dest)
			},
		},
>>>>>>> Stashed changes
	}
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
func runKamal(ctx context.Context, prefixArgs []string, args []string, lineCh chan<- string, doneCh chan<- error) {
	full := append(append([]string{}, prefixArgs...), args...)
	bin, extra, ok := kamalBinaryAvailable()
	if !ok {
		doneCh <- errBinaryNotFound
		close(lineCh)
		return
	}
	full = append(extra, full...)

	cmd := exec.CommandContext(ctx, bin, full...)
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
