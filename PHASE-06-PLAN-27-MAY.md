# PHASE-06 — Headless Daemon + Packaging

> Dated 27 May 2026. Depends on PHASE-03, PHASE-04, PHASE-05.

## Context

Up to this point, the daemon ran inside the Tauri app process. For server / Pi / "always-on-LTE" use, the user wants the daemon to run **headless** (no GUI, no webview) and for the same Rust binary to handle both modes. Phase 06 also packages everything for one-line install: Homebrew on macOS, `.deb`/`.AppImage` on Linux, curl-pipe install on Pi.

The Raspberry Pi demo is a deliberate signal: "self-hosted AI lab on a Pi 5, controlled from phone, approved from anywhere." It's an aesthetic win that plays well on HN, and it gates the cross-compilation discipline that keeps the codebase portable.

## Dependencies

- PHASE-03, PHASE-04, PHASE-05 merged.
- A Raspberry Pi 5 with Pi OS 64-bit available for testing.
- Apple Developer account is **not** required (notarisation deferred).
- Homebrew tap repo (`gongahkia/homebrew-onibi`) created on GitHub.

## Deliverables

### D1 — Headless entry point

**File**: `app/src-tauri/src/headless.rs`

```rust
pub async fn run(opts: HeadlessOpts) -> Result<()> {
    let state = AppState::new(opts.config_dir).await?;
    let port = opts.port.unwrap_or(17893);
    if opts.auto_transports {
        // Auto-enable LAN; try Tailscale if logged in; try Cloudflared if installed
        state.transports.enable("lan").await.ok();
        state.transports.enable("tailscale-funnel").await.ok();
        state.transports.enable("cloudflared").await.ok();
    }
    server::start_server(state.clone(), port).await
}
```

`main.rs` dispatch:
```rust
fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.iter().any(|a| a == "--headless") {
        let opts = clap_parse_headless(&args);
        tokio::runtime::Builder::new_multi_thread()
            .enable_all().build().unwrap()
            .block_on(headless::run(opts))
            .unwrap();
        return;
    }
    // Otherwise launch Tauri:
    tauri::Builder::default().run(generate_context!()).unwrap();
}
```

Symlink `onibid → onibi` (or just doc that `onibi --headless` is the canonical command; `onibid` is informal).

### D2 — Cross-compilation

Targets (from PHASE-00's release.yml):
- `aarch64-apple-darwin`
- `x86_64-apple-darwin`
- `x86_64-unknown-linux-gnu`
- `aarch64-unknown-linux-gnu` ← Raspberry Pi 5

For the headless binary, no GUI deps are needed. Conditional compile:
- Feature flag `gui` in `Cargo.toml`:
  ```toml
  [features]
  default = ["gui"]
  gui = ["dep:tauri"]
  ```
- Build headless-only: `cargo build --release --no-default-features`
- This produces a smaller binary suitable for the Pi.

For Pi cross-compile, use `cross` (https://github.com/cross-rs/cross):
```sh
cargo install cross
cross build --release --target aarch64-unknown-linux-gnu --no-default-features
```

In `release.yml` (already drafted in PHASE-00 D8), add jobs:
- `headless-pi`: cross-compile `onibi --headless` for `aarch64-unknown-linux-gnu`, upload `onibi-headless-<version>-linux-arm64`.

### D3 — SystemD user unit

**File**: `packaging/systemd/onibi.service`

```ini
[Unit]
Description=Onibi daemon
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/onibi --headless --auto-transports
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

Installed to `~/.config/systemd/user/onibi.service` (user unit, no root needed).

Enable:
```sh
systemctl --user daemon-reload
systemctl --user enable --now onibi.service
```

Document in `docs/install-linux.md`.

### D4 — Install scripts

**File**: `packaging/install-linux.sh`

```sh
#!/usr/bin/env bash
set -euo pipefail
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  TARGET="x86_64-unknown-linux-gnu" ;;
  aarch64) TARGET="aarch64-unknown-linux-gnu" ;;
  armv7l)  echo "armv7 not supported in v1.0 (planned v1.1)"; exit 1 ;;
  *) echo "unsupported arch: $ARCH"; exit 1 ;;
esac

VERSION="${ONIBI_VERSION:-1.5.0}"
URL="https://github.com/gongahkia/onibi/releases/download/v${VERSION}/onibi-headless-${VERSION}-${TARGET}"

echo "Downloading $URL ..."
curl -fL "$URL" -o /tmp/onibi
chmod +x /tmp/onibi
sudo mv /tmp/onibi /usr/local/bin/onibi

mkdir -p ~/.config/systemd/user
curl -fL https://raw.githubusercontent.com/gongahkia/onibi/v${VERSION}/packaging/systemd/onibi.service \
  -o ~/.config/systemd/user/onibi.service

systemctl --user daemon-reload
systemctl --user enable --now onibi.service

echo "Onibi installed. Get pairing token: onibi token show"
```

Curl-pipe: `curl -fsSL https://onibi.sh/install.sh | bash` (host the script at the project's domain, or just GH raw).

Pi-specific notes (`packaging/pi-install.sh` is a thin alias with extra env hint about $HOME/.config + udev rules if needed for USB devices, which we don't use).

### D5 — Homebrew tap

**Repo**: `gongahkia/homebrew-onibi` (created separately).

**File**: `packaging/homebrew/onibi.rb` (copied into the tap repo on each release; tap maintenance can be partly automated via `brew bump-formula-pr`).

```ruby
class Onibi < Formula
  desc "Cross-platform cockpit for local AI coding agents"
  homepage "https://github.com/gongahkia/onibi"
  version "1.5.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/gongahkia/onibi/releases/download/v1.5.0/Onibi-1.5.0-aarch64.dmg"
      sha256 "..."
    end
    on_intel do
      url "https://github.com/gongahkia/onibi/releases/download/v1.5.0/Onibi-1.5.0-x86_64.dmg"
      sha256 "..."
    end
  end

  def install
    # extract .dmg, copy .app to prefix
    # also install onibi CLI symlink to bin/
  end

  def caveats
    "Run `onibi setup` for first-time pairing."
  end

  test do
    assert_match "onibi", shell_output("#{bin}/onibi --version")
  end
end
```

Usage: `brew tap gongahkia/onibi && brew install onibi`.

### D6 — Setup wizard

**File**: `app/src-tauri/src/cli/setup.rs`

Interactive (`dialoguer` crate):
```
$ onibi setup
Welcome to Onibi setup.

[1/4] Generating bearer token… done.
[2/4] Detecting transports:
      ✓ LAN: 192.168.1.42:17893 (cert generated)
      ✓ Tailscale: detected, enabling Funnel… https://host.tailnet-abcd.ts.net/
      ✗ Cloudflared: not installed (run `brew install cloudflared` to enable)
[3/4] Which agent do you want to install hooks for?
      [x] Claude Code (detected v2.1.3)
      [ ] Codex CLI
      [ ] OpenCode
[4/4] Pair your phone:
      Scan this QR with your phone camera:
      [QR PRINTED TO TERMINAL]
      Or open: onibi://pair?token=...&host=...

Setup complete. Run `onibi status` to verify.
```

Use `qrcode` crate to print QR as ASCII (block characters).

### D7 — Status command

```
$ onibi status
Onibi 1.5.0 (gui mode)

Daemon:    running (uptime 3h 12m)
Sessions:  2 active (claude-code in ~/dev/foo, shell in ~/.config)
Approvals: 1 pending, 14 resolved (24h)

Transports:
  ✓ lan                 https://192.168.1.42:17893
  ✓ tailscale-funnel    https://host.tailnet-abcd.ts.net/
  ✗ cloudflared         not running

Paired devices:
  iPhone-15 (last seen 5m ago)

Adapters installed:
  claude-code (v2.1.3)
  codex
```

### D8 — Cross-compile CI matrix

Update `.github/workflows/release.yml`:
- Trigger: tag push `v*`.
- Jobs:
  - `build-macos-arm64`: `tauri build` on macos-14
  - `build-macos-x86_64`: `tauri build` on macos-13 (or `--target x86_64-apple-darwin` on macos-14)
  - `build-linux-x86_64`: `tauri build` on ubuntu-22.04
  - `build-linux-arm64-headless`: `cross build --target aarch64-unknown-linux-gnu --no-default-features` on ubuntu-22.04
- Artefacts uploaded to GH Release: `.dmg`, `.AppImage`, `.deb`, raw `onibi-headless-*` binary.

### D9 — Fresh-machine timed dry-run

The user's hard rule from the v1 plan carries forward: **time from `brew install onibi` (or curl-pipe on Linux) to the first phone-approved Claude Code action must be under 5 minutes on a fresh machine, with no docs read.**

Run this on:
- macOS fresh user account
- Ubuntu 22.04 fresh VM
- Raspberry Pi 5 with fresh Pi OS 64-bit

Record timings in `docs/dry-run-log.md`. If any host exceeds 5 minutes, file a follow-up issue and fix before Phase 07 launch.

### D10 — Tests

Smoke tests in CI:
- `pnpm tauri build` succeeds on macOS + Linux runners.
- `cross build --target aarch64-unknown-linux-gnu --no-default-features` succeeds.
- The resulting headless binary on Linux x86_64: spawn it on port 17894, `curl -s http://127.0.0.1:17894/v1/status` returns 401 (auth required); with bearer token, returns 200.

## Exit criteria

1. `onibi --headless` runs as a service on all three platforms (macOS, Linux, Pi 5).
2. SystemD user unit auto-starts daemon on Linux/Pi.
3. Curl-pipe installer works on Linux x86_64 + arm64.
4. Homebrew tap publishes a working formula (`brew install gongahkia/onibi/onibi` → app launches).
5. `onibi setup` wizard runs to completion on a fresh machine and produces a pairing QR.
6. Fresh-machine timed dry-run **≤ 5 min** on all three target platforms; recorded.
7. CI release workflow uploads four binaries (.dmg arm64, .dmg x86_64, .AppImage, headless-arm64) per tag.
8. `cargo build --no-default-features` produces a binary that excludes Tauri and works headless.

## Out of scope

- Notarisation of macOS .dmg (deferred to v1.0.1 unless trivially scriptable).
- Windows host (not planned).
- Arm v7 (32-bit Pi older than Pi 4) — v1.1.
- Pulling cloudflared / tailscale binaries into the install (these are user-managed).
- Auto-update on macOS / Linux — v1.1.
- Docker/Compose deployment — community contribution.

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git checkout v1.5/phase-06-packaging
# Cross-compile headless for Pi:
cross build --release --target aarch64-unknown-linux-gnu --no-default-features
# Smoke test the resulting binary on actual Pi:
scp target/aarch64-unknown-linux-gnu/release/onibi pi@<ip>:/tmp/
ssh pi@<ip> '/tmp/onibi --headless --port 17894 &'
# Full tauri build:
cd app && pnpm tauri build
# Setup wizard:
./target/release/onibi setup
```

## Reference reading

- `PHASE-03-PLAN-27-MAY.md` (`onibi setup` extends the CLI defined there).
- `PHASE-05-PLAN-27-MAY.md` (transports the wizard auto-enables).
- [Tauri bundle docs](https://v2.tauri.app/distribute/bundle/)
- [cross-rs/cross](https://github.com/cross-rs/cross)
- [Homebrew formula cookbook](https://docs.brew.sh/Formula-Cookbook)
- [systemd user units](https://wiki.archlinux.org/title/Systemd/User)
- [dialoguer crate](https://docs.rs/dialoguer) for the wizard
- [qrcode crate](https://docs.rs/qrcode) for ASCII QR

## PR template

Title: `phase-06: headless mode + packaging (macOS, Linux, Pi)`

Body:
```
## Deliverables
- [x] D1 headless entry (--headless)
- [x] D2 cross-compile targets (incl. aarch64 Pi)
- [x] D3 systemd user unit
- [x] D4 install scripts (Linux + Pi)
- [x] D5 Homebrew tap formula
- [x] D6 onibi setup wizard
- [x] D7 onibi status command
- [x] D8 release.yml updates
- [x] D9 fresh-machine timed dry-run on 3 platforms (≤5min each)
- [x] D10 smoke tests in CI

## Verification
- [x] Pi 5 dry-run ≤5 min (recording)
- [x] Ubuntu dry-run ≤5 min (recording)
- [x] macOS dry-run ≤5 min (recording)
- [x] brew install works
- [x] systemd unit autostarts after reboot

Next: PHASE-07 (hardening + launch).
```
