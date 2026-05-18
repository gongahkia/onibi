# Kelp

Kelp is a local-first planner with a Lazygit-style terminal UI and stable scriptable commands.

Running `kelp` with no subcommand opens the full-screen TUI. Existing command workflows remain available for shell scripts and agent tooling.

## Dependencies

- Zig 0.15 or newer for source builds
- curl or wget for release installs

## Install

```console
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/installer.sh -o installer.sh
$ chmod +x installer.sh
$ ./installer.sh --build-from-source
$ ./installer.sh --with-completions
```

The installer writes `kelp` to `${KELP_INSTALL_DIR:-$HOME/.local/bin}`.

```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Usage

```console
$ kelp
$ kelp init
$ kelp project add --name Launch --deadline next-week
$ kelp task add --title "Draft release notes" --project Launch --priority high --due tomorrow
$ kelp task ready
$ kelp review daily
$ kelp --output json task list
```

## TUI Keys

```text
1-5       Focus panels
j/k       Move selection
n         New task
p         New project
space     Mark selected task as next action
s/w/b     Start, wait, or block selected task
d         Complete selected task
a/r       Archive or reopen selected task
x         Delete selected task
?         Show keybindings
q         Quit
```

## Storage

Kelp keeps the existing local JSON storage contract:

- data: `$XDG_DATA_HOME/kelp/data.json` or `$HOME/.local/share/kelp/data.json`
- config: `$XDG_CONFIG_HOME/kelp/config.json` or `$HOME/.config/kelp/config.json`
- `--data-dir` colocates data and config for tests or isolated workspaces

## Test And Package

```console
$ zig build test
$ ./scripts/package-release.sh
```

## Support

| Platform | Status |
| :---: | :---: |
| macOS | Supported |
| Linux | Supported |
| Windows | Supported through WSL |
