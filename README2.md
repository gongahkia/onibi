# Kelp

`kelp` is a strict, local-first planner CLI for tasks, projects, and reviews.
It is designed for explicit shell workflows and agentic tooling that need stable JSON output, deterministic commands, and no interactive prompts.

## Install

Primary source install:

```console
cargo install kelp --locked
```

Binary releases:

- Download the archive for your platform from [GitHub Releases](https://github.com/gongahkia/kelp/releases).
- Extract it and place `kelp` somewhere on your `PATH`.

Installer:

```console
curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/installer.sh -o installer.sh
chmod +x installer.sh
./installer.sh --build-from-source
./installer.sh --release-version 1.0.0
./installer.sh --with-completions
```

Homebrew:

- A formula artifact is maintained in `Formula/kelp.rb`.
- Publishing that formula to a tap is a release-management step outside this repo.

## Quickstart

```console
kelp init
kelp project add --name Launch --deadline next-week
kelp task add --title "Draft release notes" --project Launch --priority high --due tomorrow
kelp task ready
kelp review daily
kelp review weekly
```

Useful non-interactive patterns:

```console
kelp task add --title "Write status update" --notes-file ./notes.md --tag ops --tag weekly
kelp task edit 12 --due next-monday --priority high
kelp task list --ready --limit 20
kelp task list --project Launch --tag docs --query release
kelp project list --missing-next-action
kelp project list --at-risk --deadline-within 7
kelp storage export --file ./kelp-export.json
```

## Command Model

Top-level resources:

- `task`: create, edit, list, complete, defer, archive, and query tasks.
- `project`: create, edit, list, archive, and inspect projects.
- `review`: daily and weekly review views with optional action application.
- `today`, `upcoming`, `search`, `storage`, `config`, `import`.

Date expressions:

- Absolute: `2026-03-29`
- Relative: `today`, `tomorrow`, `next-week`, `next-month`, `next-monday`, `+3d`

## JSON Contract

Request JSON with either:

```console
kelp --output json task show 12
kelp task show 12 --json
```

Every successful JSON response uses the same envelope:

```json
{
  "schema_version": 1,
  "command": "task.show",
  "data": {
    "id": 12,
    "title": "Draft release notes"
  }
}
```

Every JSON error is emitted on `stderr`:

```json
{
  "schema_version": 1,
  "error": {
    "code": "task_not_found",
    "message": "task 999 does not exist",
    "details": []
  }
}
```

## Exit Codes

- `0`: success
- `2`: usage or validation error
- `3`: not found
- `4`: state conflict or invalid transition
- `5`: storage, config, or serialization failure

## Data And Config Paths

Default layout:

- Data: `${XDG_DATA_HOME:-~/.local/share}/kelp`
- Config: `${XDG_CONFIG_HOME:-~/.config}/kelp`

Overrides:

- `KELP_DATA_DIR`
- `KELP_CONFIG_DIR`
- Global CLI flag: `--data-dir PATH`

Compatibility behavior:

- If `KELP_DATA_DIR` is set and `KELP_CONFIG_DIR` is not, config stays colocated with the data directory.
- If a legacy config exists in the old data directory location, Kelp migrates it into the default config directory on first load.

## Shell Integration

Generate completions:

```console
kelp completions bash
kelp completions zsh
kelp completions fish
```

## Release Packaging

Create release artifacts locally:

```console
./scripts/package-release.sh
```

This generates:

- a host binary archive
- a source archive
- SHA-256 checksum files
- an updated Homebrew formula artifact
