# kamal-tui

A multi-panel, terminal UI (TUI) for managing [Kamal](https://kamal-deploy.org/) deployments effortlessly. 

Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea), it provides a fast, interactive, and secure way to manage your Rails/Docker applications.

![Demo](https://github.com/stawan15/kamal-tui/assets/placeholder.png)

## 🌟 Features

- **Lazygit-style UI:** See Destinations, Actions, and Logs all in one unified screen.
- **Secure Secrets Manager:** Store Kamal secrets (like `DATABASE_URL`) directly in your OS Keychain (like Azure Pipelines). No more plaintext `.env` or `.kamal/secrets` files hanging around!
- **Interactive Confirmations:** Built-in confirmations for all commands to prevent accidental deployments or rollbacks.
- **DB Dump & Restore:** Built-in actions to quickly run `pg_dump` and `pg_restore` (Fully customizable for your stack).
- **Keyboard & Mouse Support:** Navigate lists with mouse scroll, clicks, or keyboard (`j/k`, `tab`).

## 🚀 Installation

### Option 1: Using Homebrew (macOS / Linux)
```bash
# Add the tap and install
brew tap stawan15/kamal-tui
brew install kamal-tui
```

### Option 2: Using Go
If you have Go 1.20+ installed, you can build and install it directly:
```bash
go install github.com/stawan15/kamal-tui@latest
```

## 🎮 Usage

Simply run `kamal-tui` from the root of your Rails (or any Kamal-managed) project:

```bash
cd your-rails-project
kamal-tui
```

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `d` | Quick Deploy |
| `r` | Quick Rollback (Prompts for version) |
| `l` | View App Logs |
| `s` | Open Secure Secrets Manager |
| `tab` | Switch between Destinations, Actions, and Logs panels |
| `q` | Quit |

### 🔐 Managing Secrets Securely

1. Press `s` in the TUI to open the **Secure Secrets Manager**.
2. Press `a` to add a new secret (e.g. `DATABASE_URL`).
3. Type the secret value (it will be hidden as `***`).
4. Press `Enter` to save. It is securely encrypted in your OS keychain.
5. When you run any `kamal` command from the TUI, these secrets are injected into the command's environment on the fly.
6. Press `delete` or `x` to remove a secret.

## 💾 Customizing Database Backups (Dump/Restore)

By default, the DB Dump and Restore actions in `kamal-tui` are configured for PostgreSQL (`pg_dump` and `pg_restore`). They execute inside the Kamal app container using your `$DATABASE_URL`.

If you use a different database (e.g. MySQL, SQLite) or want to run an accessory command, you can customize this by editing `kamal.go`:

```go
// In kamal.go
args = append(args, "--", "/bin/sh", "-c", "pg_dump $DATABASE_URL -F c > /tmp/db.dump")
```

## Requirements
- `kamal` command available in your PATH (or `bundle exec kamal` in a Ruby project).

## Contributing
Bug reports and pull requests are welcome on GitHub!
