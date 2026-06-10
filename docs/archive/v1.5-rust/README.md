# v1.5 Rust+Tauri+PWA — archived 2026-06-10

This directory preserves the v1.5 codebase verbatim. It never shipped.

Pivoted to v2 (Go + Telegram-only) per `TODO-10-JUN.md` at the repo root.
Rationale: antirez/tgterm validated the Telegram-bot shape with an order of
magnitude less setup friction than the v1.5 PWA+tunnels surface. The Rust
adapter logic was largely thin wrappers around hook-script templates, which
port to Go trivially. The native desktop UI was competing head-on with cmux
(18.8k★) without a clear differentiator.

Contents:

- `app/` — Tauri 2.x desktop (Rust backend, React/Vite/TS frontend, xterm.js)
- `mobile/` — PWA second-screen
- `adapters/` — hook script templates per agent (claude-code, codex, opencode,
  goose, cursor, aider, gemini, omp, pi)
- `docs/` — v1.5-era architecture diagrams, deployment guides (LAN/Tunnel),
  transport docs, security model, policy docs, install guides per platform
- `packaging/` — v1.5 Homebrew formula + systemd unit + install scripts
- `output/` — Playwright smoke-test artifacts
- `scripts/` — Tauri/Playwright dev scripts
- `script/build_and_run.sh`
- `Makefile` (v1.5)
- `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`
- `rust-toolchain.toml`
- `DESIGN.md`
- `TO-IMPLEMENT-PIVOT-4-JUN.md`
- `ci.yml.bak`, `release.yml.bak` — v1.5 GitHub Actions workflows

Git history preserves the full evolution. To recover any specific file:

```sh
git log --follow -- docs/archive/v1.5-rust/<path>
git show <commit>:<original-path>
```
