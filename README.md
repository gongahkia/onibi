# Kelp

Kelp is a Rust CLI personal planner for tasks, projects, recurring work, and daily or weekly review flows.

This rewrite replaces the old prompt-driven single-file prototype with:

- typed domain models for tasks, projects, status, priority, and recurrence
- subcommand-driven CLI workflows
- structured local JSON storage with backups, lock files, and export tooling
- richer terminal output for lists, details, and review views
- offline-testable application logic with deterministic clocks in tests

## Features

- `kelp import legacy --source <path>`
- `kelp config show|set`
- `kelp storage path|backup|export`
- `kelp task add|list|show|edit|bulk-edit|next|start|wait|block|done|reopen|defer|archive|unarchive|delete`
- `kelp project add|list|show|archive|unarchive`
- `kelp today`
- `kelp upcoming --days <n>`
- `kelp review daily --next-action <id> --start <id> --waiting <id> --blocked <id> --complete <id> --defer <id:date>`
- `kelp review weekly --archive <id> --plan <project:task>`
- `kelp search <query>`
- `kelp completions bash|zsh|fish`
- recurring task generation for daily, weekly, and monthly work
- richer planner states for `next_action`, `waiting`, and `blocked` tasks
- JSON output on planner and listing commands via `--json`
- config defaults for upcoming windows, sort order, and JSON output
- human date expressions like `today`, `tomorrow`, `next-week`, `next-monday`, and `+3d`
- duplicate-safe legacy `.kelpStorage` and `.kelpProjects` migration

## Install and Run

Build locally with Cargo:

```console
cargo build --release
```

Run directly from the repo:

```console
cargo run -- task list
```

Or install into your Cargo bin directory:

```console
cargo install --path .
```

Or use the included installer:

```console
./installer.sh
```

Install with completions at the same time:

```console
./installer.sh --with-completions
```

Build a release bundle with the binary, README, installer, and shell completion files:

```console
./scripts/package-release.sh
```

## Storage

Kelp uses JSON as its canonical local backend today. It stores `data.json` under one of these locations:

1. `$KELP_DATA_DIR/data.json` when `KELP_DATA_DIR` is set
2. `$XDG_DATA_HOME/kelp/data.json` when `XDG_DATA_HOME` is set
3. `$HOME/.local/share/kelp/data.json` by default

Initialize the storage file explicitly with:

```console
kelp init
```

All other commands also create the storage file automatically on first use.

Automatic backup snapshots are stored under `backups/` next to `data.json`, and a coarse lock file is used during writes to avoid overlapping save operations.

Kelp also stores `config.json` next to `data.json`. Use it to set planner defaults that apply across commands:

```console
kelp config set --upcoming-days 10 --task-sort priority --json-output
kelp config show
```

Inspect the storage layout:

```console
kelp storage path
```

Create a manual backup snapshot:

```console
kelp storage backup
```

Export the current dataset:

```console
kelp storage export --output ./kelp-export.json
```

Import data from the legacy Kelp format:

```console
kelp import legacy --source /path/to/old/kelp/root
```

Re-running the same import skips duplicate tasks instead of cloning them into the new store.

## Usage

Create a project:

```console
kelp project add --name "Launch"
```

Create a recurring task:

```console
kelp task add \
  --title "Weekly review" \
  --project "Launch" \
  --priority high \
  --due 2026-03-20 \
  --repeat weekly \
  --tag review \
  --tag planning
```

Use relative or named dates when it is faster than typing an ISO date:

```console
kelp task add --title "Inbox zero" --due tomorrow
kelp task add --title "Send agenda" --due next-monday
kelp task add --title "Check blockers" --due +3d
```

List open tasks:

```console
kelp task list
```

List overdue tasks as JSON:

```console
kelp task list --overdue --json
```

Complete a task and spawn its next recurring instance:

```console
kelp task done 1
```

Start or defer tasks during execution:

```console
kelp task start 2
kelp task defer 2 --days 3
```

Move tasks between richer planner states:

```console
kelp task next 2
kelp task wait 2
kelp task block 2
```

Bulk-edit multiple tasks at once:

```console
kelp task bulk-edit 1 2 3 --priority high --tag sprint --due 2026-03-21
```

Run the daily review:

```console
kelp review daily
```

Apply actions while reviewing:

```console
kelp review daily --next-action 1 --waiting 2 --blocked 3 --defer 4:2026-03-20
```

Create the next action for a stalled project during weekly review:

```console
kelp review weekly --plan Launch:"Draft launch checklist"
```

Inspect the next two weeks of work:

```console
kelp upcoming --days 14
```

Search across active tasks and projects:

```console
kelp search review
```

Generate shell completions on demand:

```console
kelp completions bash > ~/.local/share/bash-completion/completions/kelp
kelp completions zsh > ~/.zfunc/_kelp
kelp completions fish > ~/.config/fish/completions/kelp.fish
```

## Development

Run the offline test suite:

```console
cargo test --offline
```

GitHub Actions also runs cross-platform test jobs and tagged release artifact builds from [.github/workflows/ci-release.yml](.github/workflows/ci-release.yml).

The codebase is split into:

- `src/domain.rs` for typed planner entities and state transitions
- `src/legacy.rs` for legacy Kelp import parsing
- `src/storage.rs` for JSON-backed persistence
- `src/cli.rs` for Clap command parsing
- `src/app.rs` for command execution and view composition
- `src/render.rs` for terminal presentation
- `scripts/package-release.sh` for local release bundles
