# Fresh Machine Smoke

This runbook records the release dry-run for a stock macOS user and a stock
Debian/Ubuntu user. It is intentionally command-first: copy each command, save
stdout/stderr, and attach screenshots before closing the release-readiness issue.

Current verification status: real macOS and Ubuntu VM output/screenshots are not
captured in this repo yet.

Reference docs checked:

- Homebrew install prefixes and shell setup: <https://docs.brew.sh/Installation>
- Homebrew uninstall path: <https://docs.brew.sh/FAQ>
- Ubuntu package-management model for local packages/dependencies: <https://ubuntu.com/server/docs/how-to/software/package-management/>

## Artifacts To Capture

| file | source |
|---|---|
| `docs/assets/fresh-machine/macos-install.png` | terminal after install |
| `docs/assets/fresh-machine/macos-doctor-preflight.png` | `onibi doctor --mode preflight --offline` |
| `docs/assets/fresh-machine/macos-up.png` | `onibi up` printing pair URL/QR |
| `docs/assets/fresh-machine/macos-doctor-after-upgrade.png` | `onibi doctor --release --after-upgrade --offline` |
| `docs/assets/fresh-machine/macos-uninstall.png` | uninstall plan + final command |
| `docs/assets/fresh-machine/ubuntu-install.png` | terminal after install |
| `docs/assets/fresh-machine/ubuntu-doctor-preflight.png` | `onibi doctor --mode preflight --offline` |
| `docs/assets/fresh-machine/ubuntu-up.png` | `onibi up` printing pair URL/QR |
| `docs/assets/fresh-machine/ubuntu-doctor-after-upgrade.png` | `onibi doctor --after-upgrade --offline` |
| `docs/assets/fresh-machine/ubuntu-uninstall.png` | uninstall plan + final command |

Do not replace this table with prose. The release note needs one file per state.

## Common Inputs

Set these once per run:

```bash
export ONIBI_SMOKE_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
export ONIBI_SMOKE_DIR="$HOME/onibi-fresh-smoke-$ONIBI_SMOKE_DATE"
mkdir -p "$ONIBI_SMOKE_DIR"
```

Record machine details:

```bash
{
  date -u
  uname -a
  id
  command -v onibi || true
} | tee "$ONIBI_SMOKE_DIR/machine.txt"
```

Expected: `onibi` is absent before install on a true fresh machine.

## macOS 14+

Assumptions: stock user account with a shell, Homebrew present or
allowed to be installed interactively. Homebrew defaults are `/opt/homebrew` on
Apple Silicon and `/usr/local` on Intel.

### Install

If `brew` is missing, install Homebrew first:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
eval "$(/opt/homebrew/bin/brew shellenv)" 2>/dev/null || eval "$(/usr/local/bin/brew shellenv)"
```

Install Onibi from the tap:

```bash
brew install gongahkia/onibi/onibi 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-install.txt"
onibi version | tee "$ONIBI_SMOKE_DIR/macos-version.txt"
```

Expected:

- `onibi version` prints a version and commit.
- `which onibi` resolves under the Homebrew prefix.

### Initialize State

```bash
onibi status --json --no-doctor --no-hooks >"$ONIBI_SMOKE_DIR/macos-status-initial.json"
```

Expected: command exits `0` and creates the local state directory.

### Hooks Install

Preview first:

```bash
onibi install-hooks --dry-run 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-hooks-dry-run.txt"
```

If supported agents are present on the machine:

```bash
onibi install-hooks --all 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-hooks-install.txt"
onibi hooks --show --all 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-hooks-show.txt"
```

Expected:

- No destructive shell changes in dry-run output.
- Installed hooks reference the Homebrew `onibi-notify` path.

### Doctor Before Pairing

```bash
onibi doctor --mode preflight --offline --color=never 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-doctor-preflight.txt"
```

Expected shape before `onibi up` has generated certs or started a service:

```text
[PASS] state dir: ...
[PASS] store key: present
[PASS] sqlite db: ...
[PASS] transport: lan
[WARN] local certs: not generated; run onibi up
[WARN] unix socket: not running
[WARN] service: not installed
[PASS] hooks: ...
```

`WARN` for local certs/socket/service is acceptable at this step.

### Up And Pair

Use LAN first. Use an iPhone hotspot when managed Wi-Fi blocks peer traffic.

```bash
onibi up --transport=lan --log-file "$ONIBI_SMOKE_DIR/macos-up.log" 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-up.txt"
```

Expected:

- Output prints the local CA profile path.
- Output prints a `https://<lan-ip>:8443/pair/<token>` URL and QR.
- Phone Safari can pair after the local CA profile is installed and fully trusted.

Stop with `Ctrl-C` after pairing and a simple terminal command.

### Doctor After Upgrade

```bash
onibi doctor --release --after-upgrade --offline --color=never 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-doctor-after-upgrade.txt"
```

Expected:

- Store key and sqlite checks pass.
- Local certs pass after successful `onibi up`.
- Hook drift is absent or reports a specific fix command.

### Uninstall

```bash
onibi uninstall --dry-run --service --hooks --all-hooks --state 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-uninstall-dry-run.txt"
onibi uninstall --yes --service --hooks --all-hooks --state 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-uninstall.txt"
brew uninstall onibi 2>&1 | tee "$ONIBI_SMOKE_DIR/macos-brew-uninstall.txt"
```

Optional Homebrew removal for a throwaway machine:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/uninstall.sh)"
```

Expected: Onibi state, hooks, service files, and binaries are removed.

## Debian/Ubuntu 24.04

Assumptions: an x86_64 Ubuntu 24.04 VM and a non-root user account. `sudo` is
used only for distro packages when the base VM lacks required tools. If the VM
policy forbids `sudo`, preinstall `curl`, `tar`, `gpg`, `ca-certificates`, and
`tmux` in the image.

The Linux arm64 artifact is the constrained SSH runtime and supports only
`onibi up`, `onibi version`, and `onibi fleet`; use
[`ssh-transport.md`](./ssh-transport.md) and `scripts/ssh-smoke.sh` for that
target. Do not use it for this full lifecycle runbook.

### Install

Install prerequisites:

```bash
sudo apt update
sudo apt install -y ca-certificates curl gnupg tar tmux
```

Install from the rendered release installer after `get.onibi.sh` is published:

```bash
curl -fsSL https://get.onibi.sh | sh 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-install.txt"
export PATH="$HOME/.local/bin:$PATH"
onibi version | tee "$ONIBI_SMOKE_DIR/ubuntu-version.txt"
```

If the installer endpoint is not published, use a release tarball instead:

```bash
tag=vX.Y.Z
arch="$(uname -m)"
case "$arch" in x86_64) ;; *) echo "fresh-machine lifecycle smoke requires x86_64, got $arch" >&2; exit 1 ;; esac
curl -fL -o /tmp/onibi.tgz "https://github.com/gongahkia/onibi/releases/download/$tag/onibi_${tag#v}_linux_${arch}.tar.gz"
mkdir -p "$HOME/.local/bin"
tar -xzf /tmp/onibi.tgz -C "$HOME/.local/bin" onibi onibi-notify
```

Expected:

- `onibi version` prints a version and commit.
- `command -v onibi` resolves under `$HOME/.local/bin`.

### Initialize State

```bash
onibi status --json --no-doctor --no-hooks >"$ONIBI_SMOKE_DIR/ubuntu-status-initial.json"
```

Expected: command exits `0` and creates `~/.local/share/onibi`.

### Hooks Install

```bash
onibi install-hooks --dry-run 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-hooks-dry-run.txt"
```

If supported agents are present:

```bash
onibi install-hooks --all 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-hooks-install.txt"
onibi hooks --show --all 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-hooks-show.txt"
```

Expected: installed hooks reference `$HOME/.local/bin/onibi-notify`.

### Doctor Before Pairing

```bash
onibi doctor --mode preflight --offline --color=never 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-doctor-preflight.txt"
```

Expected shape before `onibi up`:

```text
[PASS] state dir: ...
[PASS] store key: present
[PASS] sqlite db: ...
[PASS] transport: lan
[WARN] local certs: not generated; run onibi up
[WARN] unix socket: not running
[WARN] service: not installed
[PASS] hooks: ...
```

### Up And Pair

```bash
onibi up --transport=lan --log-file "$ONIBI_SMOKE_DIR/ubuntu-up.log" 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-up.txt"
```

Expected:

- Output prints the local CA profile path.
- Output prints a `https://<lan-ip>:8443/pair/<token>` URL and QR.
- Phone browser pairs over the same LAN or hotspot.

Stop with `Ctrl-C` after pairing and a simple terminal command.

### Doctor After Upgrade

```bash
onibi doctor --after-upgrade --offline --color=never 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-doctor-after-upgrade.txt"
```

Expected:

- Store key and sqlite checks pass.
- Local certs pass after successful `onibi up`.
- Hook drift is absent or reports a specific fix command.

### Uninstall

```bash
onibi uninstall --dry-run --service --hooks --all-hooks --state 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-uninstall-dry-run.txt"
onibi uninstall --yes --service --hooks --all-hooks --state 2>&1 | tee "$ONIBI_SMOKE_DIR/ubuntu-uninstall.txt"
rm -f "$HOME/.local/bin/onibi" "$HOME/.local/bin/onibi-notify"
```

Expected: `command -v onibi` fails after opening a new shell or removing the path entry.

## Release Artifact Smoke

Before real-machine runs, keep running the local artifact gate:

```bash
goreleaser release --snapshot --clean
scripts/release-smoke.sh dist
scripts/fresh-machine-doc-check.sh
```

`scripts/release-smoke.sh` verifies checksums, archive membership, host-platform
binary execution, preflight doctor in an isolated HOME, and SSH size-only smoke.
`scripts/fresh-machine-doc-check.sh` verifies this runbook still names every
required screenshot, transcript command, platform section, and release reference.
After the real macOS and Ubuntu runs, use:

```bash
scripts/fresh-machine-doc-check.sh --require-artifacts --smoke-dir "$ONIBI_SMOKE_DIR"
```

That stricter mode fails until the repo contains real PNG screenshots and the
run directory contains the expected transcripts.

## Close Criteria

Do not close the tracking issue until all are true:

- macOS transcript files exist under the run directory and screenshots are added.
- Ubuntu transcript files exist under the run directory and screenshots are added.
- macOS captures `onibi doctor --release --after-upgrade --offline`; Ubuntu captures `onibi doctor --after-upgrade --offline` because Linux is beta-only.
- The uninstall step proves no Onibi binary, service, hook, or state remains.
