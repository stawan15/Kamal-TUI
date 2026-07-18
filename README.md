# kamal-tui

[![Go Report Card](https://goreportcard.com/badge/github.com/stawan15/kamal-tui)](https://goreportcard.com/report/github.com/stawan15/kamal-tui)
[![GitHub Release](https://img.shields.io/github/v/release/stawan15/kamal-tui)](https://github.com/stawan15/kamal-tui/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A `lazygit`-style Terminal UI (TUI) dashboard for [Kamal](https://kamal-deploy.org).

Instead of running `kamal deploy -d destination` blindly and waiting, `kamal-tui` provides a rich interactive interface that lets you select your destination, perform actions (deploy, rollback, logs, etc.), and watch the streamed output all on one screen.

![Demo](demo.gif)

## Features

- **Multi-Panel Interface:** See your destinations, actions, and output streams simultaneously.
- **Mouse & Keyboard Support:** Click on elements or use handy keybindings (`d` for deploy, `r` for rollback, `l` for logs).
- **Fast Navigation:** Use `Tab` or `h`/`l` to jump between panels.
- **Streaming Output:** View `kamal` command output directly in the dashboard without switching context.

## Installation

### Using Homebrew (macOS / Linux)

```bash
brew install stawan15/tap/kamal-tui
```

### Using Go

```bash
go install github.com/stawan15/kamal-tui@latest
```

### Binary Release

Download the pre-compiled binary for your OS/Architecture from the [Releases page](https://github.com/stawan15/kamal-tui/releases).

## Usage

Simply run `kamal-tui` from the root of your project where `config/deploy.yml` (and other destination configs like `config/deploy.staging.yml`) is located.

```bash
kamal-tui
```

### Keybindings

- **`Tab`, `h`, `l`**: Switch focus between panels (Destinations / Actions / Logs)
- **`d`**: Quick deploy the selected destination
- **`r`**: Quick rollback the selected destination
- **`l`**: Quick view logs for the selected destination
- **`Enter`**: Execute the highlighted action on the selected destination
- **`q`, `Ctrl+C`**: Quit
- **`Esc`**: Cancel current input / operation

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how you can help!
