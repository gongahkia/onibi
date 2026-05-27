# PHASE-00 — Foundation & Demolition

> Authoritative for Onibi v1.5 re-pivot. Dated 27 May 2026. Supersedes `docs/archive/WORKON-PIVOT-ASAP.md`.

## Context

Onibi has pivoted twice. v0 was a macOS-only Ghostty notifier (Swift). v1 (planned, never coded) was a Go daemon + Swift menubar + React PWA for AI-agent approval gating. **v1.5 (this plan)** rebuilds the project as a **Tauri (Rust + system webview) desktop cockpit for AI coding agents** with a **mobile PWA second-screen** for mirror + approval.

The competitive landscape moved: [cmux (manaflow-ai/cmux)](https://github.com/manaflow-ai/cmux) already owns the macOS-native + libghostty + vertical-agent-tabs space with 18.8k stars. Onibi's differentiation is now:
- **Cross-platform**: macOS + Linux (incl. Raspberry Pi) via Tauri, not macOS-only.
- **Mobile second-screen**: phone approval inbox + live terminal mirror — cmux has none.
- **Multi-transport**: Tailscale Funnel **+** Cloudflare Tunnel **+** LAN/self-signed/QR, all first-class.

Phase 00 is non-negotiably first. No Rust code is written before this phase's exit criteria are green. The point of Phase 00 is to clear cognitive debt: delete the dead v0 Swift, archive the v1 planning docs, scaffold the new Tauri project, get CI green on a hello-world build.

## Dependencies (must be true before starting)

- Clean working tree on `main` (currently is at commit `f6e563a`).
- GitHub CLI (`gh`) authenticated against `gongahkia/onibi`.
- macOS 14+ host with: Rust 1.75+, Node 20+, pnpm 8+, Tauri CLI 2.x.
- Permission to force-push tags and to close GH issues #6–#29.

## Deliverables

### D1 — Git hygiene
- `git tag -a v0-archive -m "Archive of v0 Swift + planned v1 daemon. Pre-Tauri pivot." f6e563a && git push origin v0-archive`
- `git checkout -b v1.5 && git push -u origin v1.5`
- All Phase 00 work commits onto branch `v1.5/phase-00-foundation`, merged into `v1.5` via PR.

### D2 — Delete v0 Swift surface
Remove the following with `git rm -r`:
- `Onibi/`
- `OnibiCore/`
- `OnibiSessionProxy/`
- `OnibiTests/`
- `OnibiCoreTests/`
- `Package.swift`
- `Package.resolved`
- `Onibi.xcodeproj/` (if present)
- Swift portions of `Makefile` (keep Make as a thin orchestrator; rewrite in D6).

### D3 — Archive planning + reference material
Move (not delete; preserve under `docs/archive/`):
- `WORKON-PIVOT-ASAP.md` → `docs/archive/WORKON-PIVOT-ASAP.md`
- `README.md` → `docs/archive/v0-README.md`
- `SPEC.md` → `docs/archive/SPEC-v1-draft.md` (the Phase 1 daemon spec; will be re-cut from scratch in PHASE-03)
- `OnibiWeb/` → `docs/archive/OnibiWeb/` (entire directory; mobile rebuilt from scratch in PHASE-04, but `src/lib/ghosttyTerminal.ts` is referenced reading for the new web team)
- `docs/phases/` → `docs/archive/v1-phases/` (the four Phase 1-4 specs from v1 plan)
- `docs/security.md` (if present) → `docs/archive/security-v1.md`

### D4 — Tauri scaffold
Create directory `app/`. Use Tauri 2.x with React + Vite + TypeScript template:

```
cd /Users/gongahkia/Desktop/coding/projects/onibi
pnpm create tauri-app@latest app -- --template react-ts --manager pnpm --identifier sh.onibi.desktop
```

Edit `app/src-tauri/tauri.conf.json`:
- `productName`: "Onibi"
- `identifier`: "sh.onibi.desktop"
- `version`: "1.5.0-dev"
- `app.windows[0].title`: "Onibi"
- `app.windows[0].width`: 1400, `height`: 900
- `bundle.targets`: `["app", "dmg", "appimage", "deb"]`

Edit `app/src-tauri/Cargo.toml` package metadata:
- name: `onibi`
- description: "Desktop cockpit for local AI coding agents, with mobile second-screen"
- license: "Apache-2.0"

`app/package.json`:
- name: `onibi-app`
- scripts: `dev`, `build`, `tauri`, `test`, `typecheck`

Verify: `cd app && pnpm install && pnpm tauri dev` opens a Tauri window with the default React template. Quit.

### D5 — Mobile PWA scaffold
Create directory `mobile/`. Vite + React + TypeScript + PWA plugin:

```
cd /Users/gongahkia/Desktop/coding/projects/onibi
pnpm create vite@latest mobile -- --template react-ts
cd mobile
pnpm add vite-plugin-pwa workbox-window
pnpm install
```

`mobile/package.json` name: `onibi-mobile`.
`mobile/vite.config.ts` registers `vite-plugin-pwa` with manifest stub:
- name: "Onibi Mobile"
- short_name: "Onibi"
- theme_color: "#0b0e14"
- icons: placeholders (replaced in PHASE-04)

Verify: `cd mobile && pnpm dev` serves on `http://localhost:5173`, no errors.

### D6 — Repo root scaffolding
Create:
- `README.md` (new, terse v1.5 pitch + status badges + link to PHASE-XX docs)
- `LICENSE` (Apache-2.0 — copy from current repo if present, else fetch)
- `Makefile` rewritten:
  - `make dev-app` → `cd app && pnpm tauri dev`
  - `make dev-mobile` → `cd mobile && pnpm dev`
  - `make test` → `cd app/src-tauri && cargo test && cd ../.. && cd mobile && pnpm test && cd ../app && pnpm test`
  - `make build` → tauri build + mobile build
  - `make clean`
- `.gitignore` additions: `target/`, `dist/`, `dist-ssr/`, `node_modules/`, `*.local`, `.DS_Store`, `pnpm-lock.yaml` is **kept** (deterministic builds).
- `.editorconfig` (4-space indent for Rust, 2 for JS/TS, LF line endings).
- `rust-toolchain.toml` pinning to `stable` (no nightly).
- `pnpm-workspace.yaml` declaring `app` and `mobile` as workspaces (allows shared dev tooling).

### D7 — Phase docs in repo root
Confirm all eight `PHASE-XX-PLAN-27-MAY.md` files (00 through 07) and `PROMPTS-27-MAY.md` are committed to the repo root. They MUST be present at branch tip before PR merge.

### D8 — CI rewrite
Replace `.github/workflows/*.yml` with two files:

**`.github/workflows/ci.yml`** — runs on push + PR:
- Matrix: `os: [macos-14, ubuntu-22.04]`
- Jobs:
  - `rust`: `cargo fmt --check`, `cargo clippy --all-targets --all-features -- -D warnings`, `cargo test --workspace`
  - `app-frontend`: `pnpm install`, `pnpm --filter onibi-app typecheck`, `pnpm --filter onibi-app test`
  - `mobile`: `pnpm --filter onibi-mobile typecheck`, `pnpm --filter onibi-mobile test`
  - `tauri-build-smoke`: `pnpm --filter onibi-app tauri build --debug` on each OS (catches build-system breakage early)
- Cache: cargo registry, target/, pnpm store.
- Required for PR merge.

**`.github/workflows/release.yml`** — runs on tag `v*`:
- Cross-compile matrix: `darwin-x86_64`, `darwin-aarch64`, `linux-x86_64`, `linux-aarch64`.
- Outputs: `onibi-<version>-<target>.{dmg,AppImage,deb}` uploaded to GH Release.
- Headless binary (PHASE-06 will use this).

Delete every other workflow file from `.github/workflows/` (old Swift CI).

### D9 — Close superseded issues
For each open GH issue (#6 through #29) tagged `pivot:v1`:
```
gh issue close <N> --comment "Superseded by PHASE-XX-PLAN-27-MAY.md re-pivot (v1.5). See repo root for new phase docs."
```
List of issues to close: `6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29`.

### D10 — New README (placeholder OK)
`README.md` content (terse, ~30 lines):
- Pitch line: "Onibi — cross-platform cockpit for local AI coding agents, with a phone in your pocket."
- Status: 🚧 v1.5 in development. Track progress in `PHASE-XX-PLAN-27-MAY.md`.
- Quick architecture (1 paragraph + diagram link).
- Differentiation: vs cmux (cross-platform + mobile), vs Aider/Codex (we're not the agent, we're the harness).
- License: Apache-2.0.
- Roadmap links to phase docs.

Final marketing README replaces this in PHASE-07.

### D11 — `docs/archive/README.md`
One-line index pointing at the v0 archive material so a future reader knows the chronology.

## Exit criteria

All of the following must be true before PHASE-01 begins:

1. `git tag v0-archive` exists on origin.
2. Branch `v1.5` exists on origin; default branch on GitHub remains `main` until PHASE-07 final merge.
3. `find . -name '*.swift' -not -path './docs/archive/*' | wc -l` returns `0`.
4. `Onibi/`, `OnibiCore/`, `OnibiSessionProxy/`, `OnibiTests/`, `OnibiCoreTests/`, `Package.swift` directories/files do not exist in working tree.
5. `app/` and `mobile/` directories exist with passing `pnpm install`.
6. `cd app && pnpm tauri dev` launches a window (manual verification, screenshot in PR).
7. CI green: `.github/workflows/ci.yml` passes on both macOS and Ubuntu runners for the merge commit.
8. `gh issue list --state open --label pivot:v1 | wc -l` returns `0`.
9. All eight `PHASE-XX-PLAN-27-MAY.md` files + `PROMPTS-27-MAY.md` exist at repo root.

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git fetch --all --tags
git tag --list | grep -q '^v0-archive$' && echo "TAG OK"
git branch -a | grep -q 'v1.5' && echo "BRANCH OK"
test ! -d Onibi && test ! -d OnibiCore && echo "SWIFT GONE"
test -d app/src-tauri && test -d mobile && echo "SCAFFOLD OK"
(cd app && pnpm install --frozen-lockfile && pnpm typecheck) && echo "APP OK"
(cd mobile && pnpm install --frozen-lockfile && pnpm typecheck) && echo "MOBILE OK"
gh issue list --state open --label pivot:v1 --json number | jq 'length == 0' && echo "ISSUES CLOSED"
ls PHASE-0{0,1,2,3,4,5,6,7}-PLAN-27-MAY.md PROMPTS-27-MAY.md && echo "DOCS PRESENT"
```

## Out of scope (do NOT do in PHASE-00)

- Writing any Rust code beyond what `cargo tauri init` generates.
- Writing any frontend React components beyond `App.tsx` placeholder.
- Implementing PTY spawning (that's PHASE-01).
- Touching `SPEC.md` (it's been archived; new spec is PHASE-03).
- Implementing any adapter logic.
- Setting up Tailscale, Cloudflare, or LAN transport (PHASE-05).
- Drawing the final architecture diagram (PHASE-07).
- Notarising the macOS build.

## Reference reading

- `docs/archive/WORKON-PIVOT-ASAP.md` (the v1 plan being superseded — read for context on what was tried)
- `docs/archive/SPEC-v1-draft.md` (Phase 03 will rewrite this — read to understand prior protocol intent)
- [Tauri 2.x getting-started](https://v2.tauri.app/start)
- [Tauri React+Vite template docs](https://v2.tauri.app/start/create-project/)
- [vite-plugin-pwa](https://vite-pwa-org.netlify.app)
- [cmux source for layout reference](https://github.com/manaflow-ai/cmux)

## PR template for Phase 00

Title: `phase-00: foundation + demolition`

Description body:
```
Closes the v0 chapter. Sets up Tauri + Vite scaffolds. CI green on hello-world.

## Deliverables
- [x] D1 git tag v0-archive + branch v1.5
- [x] D2 delete v0 Swift
- [x] D3 archive planning docs to docs/archive/
- [x] D4 Tauri scaffold under app/
- [x] D5 Mobile PWA scaffold under mobile/
- [x] D6 root files (README, LICENSE, Makefile, .gitignore, .editorconfig)
- [x] D7 8 PHASE docs + PROMPTS doc in repo root
- [x] D8 CI rewrite (.github/workflows/{ci,release}.yml)
- [x] D9 close v1 issues
- [x] D10 placeholder README
- [x] D11 docs/archive/README.md index

## Verification
- [x] CI green
- [x] `pnpm tauri dev` opens a window (screenshot)
- [x] `pnpm dev` in mobile/ serves
- [x] No `.swift` files outside archive
- [x] All v1 issues closed

Next: PHASE-01 (PTY core + xterm).
```
