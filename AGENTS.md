# AGENTS.md

## Project overview

`kamal-tui` is a Go 1.22 terminal UI for operating Kamal-managed applications. It
uses Bubble Tea for the event loop and UI, Bubbles for widgets, Lip Gloss for
styling, `go-keyring` for project-scoped secrets, and `godotenv` for loading
Kamal environment files.

The program is a single `main` package. It is normally launched from the root
of a Rails/Docker project that contains Kamal configuration, not necessarily
from this repository's directory.

## Repository map

- `main.go` — Bubble Tea model, event handling, panels, overlays, keyboard
  shortcuts, command lifecycle, and rendering.
- `kamal.go` — Kamal action definitions, destination discovery, executable
  lookup, subprocess execution, streamed output, and environment loading.
- `dashboard.go` — deploy YAML parsing and Docker stats collection locally or
  over SSH.
- `secrets.go` — project-scoped OS keychain storage for secrets.
- `styles.go` — shared Tokyo Night-inspired Lip Gloss styles and colors.
- `errors.go` — shared errors shown by the UI.
- `README.md` — user-facing installation, usage, shortcuts, and database
  backup documentation.
- `.github/workflows/ci.yml` — CI checks.
- `.goreleaser.yaml` — cross-platform release and Homebrew packaging.

## Development workflow

Use the repository's Go toolchain and keep `go.mod`/`go.sum` consistent.

```bash
go build ./...
go vet ./...
go test ./...
```

For a local binary:

```bash
go build -o kamal-tui .
./kamal-tui
```

To work on the dashboard without Docker, SSH, Kamal, or a project config:

```bash
go run . --dev
```

Press `p` to open the dashboard. Developer mode uses deterministic mock data
and command output, displays `DEV MODE` in the footer, and is also available
through `KAMAL_TUI_DEV=1`.

Before a release-related change, also verify the GoReleaser configuration if
GoReleaser is installed:

```bash
goreleaser check
```

There are currently no repository tests, but new behavior should include unit
tests where practical, especially for argument construction, destination
discovery, environment precedence, YAML parsing, and Docker-stat parsing.

## Implementation guidance

- Keep the Bubble Tea update/view flow in `main.go`; put Kamal and deployment
  concerns in `kamal.go` and dashboard concerns in `dashboard.go`.
- Prefer small, pure helpers for parsing and command construction so they can be
  tested without launching the TUI or external commands.
- Preserve streamed stdout/stderr behavior for long-running Kamal commands and
  ensure goroutines/channels are closed exactly once.
- Use `context.Context` and cancellation for subprocesses and dashboard polling.
- Keep UI mutations on the Bubble Tea update path; do not introduce unsynchronized
  model mutations from worker goroutines.
- Follow standard Go formatting (`gofmt`) and idiomatic error wrapping. Keep
  comments focused on non-obvious behavior.
- Add or update README documentation when changing shortcuts, actions,
  installation, configuration, or user-visible behavior.

## Kamal and security-sensitive behavior

- `discoverDestinations` reads `config/deploy.yml` and
  `config/deploy.<name>.yml`; retain the default destination even when named
  destinations exist.
- `runKamal` locates `kamal`, `bin/kamal`, or `bundle exec kamal`, then streams
  combined output. Do not bypass the existing confirmation flow for destructive
  or deployment actions.
- Environment files are loaded in `loadEnvForDest`; keychain values intentionally
  override file values. Never log secret values or put them in command-line
  arguments when an environment variable will work.
- Secrets are stored in the OS keychain under a project ID derived from the
  working directory. Keep secret input masked and avoid changing the keyring
  service/account scheme without a migration plan.
- The dashboard currently invokes SSH with batch mode and parses Kamal deploy
  YAML. Preserve bounded timeouts and be careful when changing remote command
  construction.
- Multi-server app logs use Kamal's `--hosts` option. Keep the aggregate log
  view as the default and preserve `[`/`]` server paging when changing the log
  action or destination handling.
- Database dump/restore and `kamal remove` affect real deployments. New actions
  should be explicit, confirmed, and documented.

## Validation checklist

For a normal code change:

1. Run `gofmt` on changed Go files.
2. Run `go build ./...`, `go vet ./...`, and `go test ./...`.
3. Exercise the relevant UI path manually when it depends on terminal layout,
   keychain behavior, Kamal, Docker, SSH, or a real project configuration.
4. Confirm no secrets, generated binaries, `dist/` contents, or local `.env`
   files were added to the diff.

For release changes, run the CI checks and review `.goreleaser.yaml` together
with the GitHub release workflow before creating a tag.
