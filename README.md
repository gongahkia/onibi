# Kelp

Kelp is a Rust CLI personal planner for tasks, projects, recurring work, and daily or weekly review flows.

This rewrite replaces the old prompt-driven single-file prototype with:

- typed domain models for tasks, projects, status, priority, and recurrence
- subcommand-driven CLI workflows
- structured local JSON storage
- richer terminal output for lists, details, and review views
- offline-testable application logic with deterministic clocks in tests

## Features

- `kelp task add|list|show|edit|done|reopen|delete`
- `kelp project add|list|show|archive`
- `kelp today`
- `kelp upcoming --days <n>`
- `kelp review daily`
- `kelp review weekly`
- `kelp search <query>`
- recurring task generation for daily, weekly, and monthly work
- JSON output on planner and listing commands via `--json`

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

## Storage

Kelp stores data in `data.json` under one of these locations:

1. `$KELP_DATA_DIR/data.json` when `KELP_DATA_DIR` is set
2. `$XDG_DATA_HOME/kelp/data.json` when `XDG_DATA_HOME` is set
3. `$HOME/.local/share/kelp/data.json` by default

Initialize the storage file explicitly with:

```console
kelp init
```

All other commands also create the storage file automatically on first use.

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

Run the daily review:

```console
kelp review daily
```

Inspect the next two weeks of work:

```console
kelp upcoming --days 14
```

Search across active tasks and projects:

```console
kelp search review
```

## Development

Run the offline test suite:

```console
cargo test --offline
```

The codebase is split into:

- `src/domain.rs` for typed planner entities and state transitions
- `src/storage.rs` for JSON-backed persistence
- `src/cli.rs` for Clap command parsing
- `src/app.rs` for command execution and view composition
- `src/render.rs` for terminal presentation
