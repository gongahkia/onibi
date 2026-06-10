# Phase 07 Final QA Log

Limit: 5 minutes from install command to first authenticated `onibi status` / pairing-ready daemon.

| Platform | Host | Install path | Duration | Recording | Result |
| --- | --- | --- | ---: | --- | --- |
| macOS | Pending fresh user account | Homebrew | Pending | Pending | Pending |
| Ubuntu 22.04 | Pending fresh VM | curl-pipe | Pending | Pending | Pending |
| Raspberry Pi 5 | Pending Pi OS 64-bit | curl-pipe | Pending | Pending | Pending |

Non-gating local smoke on 2026-05-27:

| Platform | Command | Duration | Result |
| --- | --- | ---: | --- |
| macOS development machine | `scripts/dry-run-fresh-machine.sh` | 16s | Passed headless build, setup, daemon launch, unauthenticated 401, authenticated 200, CLI status |

Transport observations from the local smoke:

| Transport | Result |
| --- | --- |
| LAN | Running locally at `https://192.168.1.26:17894/` with generated cert fingerprint. |
| Cloudflare Quick Tunnel | URL was observed, but the child process exited during the smoke run. Needs a manual LTE phone test before launch. |
| Tailscale Funnel | Not tested locally because `tailscale` was not on `PATH`. Needs a tailnet-enabled host before launch. |

Automated checks on 2026-05-27:

| Check | Result |
| --- | --- |
| `cargo fmt --check` | Passed |
| `cargo check --all-targets --all-features` | Passed |
| `cargo clippy --all-targets --all-features -- -D warnings` | Passed |
| `cargo test --workspace` | Passed: 20 tests |
| `pnpm --filter onibi-app typecheck` | Passed |
| `pnpm --filter onibi-app test` | Passed: 12 files / 19 tests |
| `pnpm --filter onibi-mobile typecheck` | Passed |
| `pnpm --filter onibi-mobile test` | Passed: 1 file / 1 test |
| `pnpm --filter onibi-app build` | Passed with a Vite large-chunk warning |
| `pnpm --filter onibi-mobile build` | Passed |
| `cargo audit` | No vulnerable crates reported; advisory warnings remain for transitive Tauri/GTK and unmaintained crates |
| `pnpm audit --prod` | Passed: no known vulnerabilities |
| `cargo run -- doctor` | Ran successfully; local machine has no Onibi token yet, Aider/Cursor/Goose not on PATH |

Local smoke helper:

```sh
scripts/dry-run-fresh-machine.sh
```

Launch blockers still requiring external/manual confirmation:

- Replace pending macOS, Ubuntu, and Pi rows with true fresh-machine runs.
- Verify Homebrew and curl-pipe one-liners from a clean environment after release binaries exist.
- Test Tailscale Funnel with a logged-in tailnet host.
- Test Cloudflare Quick Tunnel from phone-on-LTE.
- Test LAN certificate install on a real iPhone/Android device.
- Replace the generated storyboard screencast with real desktop + phone footage if the launch requires a live-device recording.

Any true fresh-machine run over 300 seconds requires a follow-up issue and a fix before launch.
