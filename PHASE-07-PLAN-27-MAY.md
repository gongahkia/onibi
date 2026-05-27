# PHASE-07 — Hardening, Docs, Launch

> Dated 27 May 2026. Sequential after PHASE-06. Final phase before v1.5.0 ships.

## Context

Phase 07 is the last mile. Everything functional is built. Phase 07 hardens what exists, writes the launch materials, and ships v1.5.0 publicly via Hacker News and X.

Treat Phase 07 as a checklist phase: no architectural decisions left. If a Phase 07 task surfaces a missing protocol field or unhandled edge case, it goes to the v1.6 issue tracker, not back to PHASE-03.

## Dependencies

- All of PHASE-00 through PHASE-06 merged.
- Recorded fresh-machine dry-runs on macOS + Linux + Pi 5 (PHASE-06 D9 deliverable).
- Branch `v1.5` ready to merge into `main` post-launch.

## Deliverables

### D1 — Security hardening pass

Audit and document the following in `docs/security.md` (rewrite from scratch; old `docs/archive/security-v1.md` is reference only):

**Threat model**:
- Adversary on local network: can attempt to hit LAN HTTPS, blocked by self-signed cert pinning + bearer token.
- Adversary on public internet (Tailscale Funnel or Cloudflared URL): URL is unguessable (Cloudflared randomises; Tailscale tailnet name is per-account). Token is bearer-only mandatory.
- Malicious local app on the same machine: can read `~/.config/onibi/token.txt` if Secret Service / Keychain not available. Document fallback risk; recommend keychain.
- Malicious agent / hook injection: agents can post arbitrary JSON; daemon validates schema before storing or surfacing to UI. No `eval` of any received payload.

**Hardening tasks**:
- Rate-limit `/v1/approval/request` to 10 req/s per source IP (use `tower-governor`).
- Body size limit: 1 MB on approval request, 5 MB on `/v1/pty/output`.
- WS message size limit: 256 KB.
- HSTS header on transport-bound endpoints (LAN excepted because self-signed).
- Content Security Policy on served PWA (`/m/*`).
- Audit dependencies: `cargo audit`, `pnpm audit`. Fix any high/critical CVE before launch.
- Disable verbose errors in release builds (strip stack traces from HTTP error responses).

### D2 — Diagnostics

Add `onibi doctor` CLI subcommand:
- Checks: token present in keychain, sqlite DB writable, port 17893 available, each adapter's external binary detectable + version.
- Output: green/yellow/red status per check + remediation hints.
- Use for support: "run `onibi doctor` and paste output to the issue."

### D3 — Architecture diagram

**File**: `docs/architecture.png` (also commit source as `docs/architecture.excalidraw` or `.drawio`)

Replaces `asset/reference/architecture.png` from the v0 era.

Content: the ASCII diagram from `WORKON-PIVOT-ASAP.md` / the meta-plan's "Architecture overview", rendered as a clean SVG/PNG. Boxes:
- Agents (Claude Code, Codex, …)
- Onibi (Tauri or headless)
  - PTY manager
  - Adapters
  - Approval store
  - WS hub
  - Transport mux
- Desktop UI (React + xterm.js)
- Mobile PWA (React + service worker)

Arrows labelled with protocol (HTTP `POST /v1/approval/request`, WS `pty-output`, etc.).

### D4 — README rewrite (final)

**File**: `README.md` (replaces the placeholder from PHASE-00 D10).

Structure:
1. **Hero**: pitch line ("Onibi — cockpit for local AI coding agents, with a phone in your pocket.")
2. **Screencast**: embedded GIF or mp4 link (the 90-second demo).
3. **Install**:
   ```
   # macOS
   brew tap gongahkia/onibi
   brew install onibi

   # Linux / Pi
   curl -fsSL https://onibi.sh/install.sh | bash
   ```
4. **Quick start**: `onibi setup` → scan QR → pair → run `claude code` → enjoy.
5. **Architecture**: embed `docs/architecture.png`.
6. **Support matrix**: agents (✓ approval / 〜 mirror / ✗), platforms (macOS, Linux, Pi), transports (Tailscale, Cloudflare, LAN).
7. **Differentiation**: vs cmux (cross-platform + mobile), vs vanilla terminals (approval gating).
8. **Roadmap**: link to issues by milestone (v1.6, v2.0).
9. **License**: Apache-2.0.
10. **Acknowledgements**: cmux (inspiration), Ghostty (terminal engine reference), Tailscale, Cloudflare.

### D5 — Adapter docs

**File**: `docs/adapters.md`

Per-agent section. Each contains:
- Install command: `onibi adapter install <name>`
- Hook surface used
- Supported features (approval intercept / edit / mirror)
- Known limitations
- Minimum version
- Sample interaction

Agents covered (per Phase 03 outcome):
- Claude Code: Full
- Codex CLI: Bash-only intercept; apply_patch not interceptable
- OpenCode: per spike result
- Gemini CLI: per spike result
- Aider: per spike result
- Cursor agent: per spike result
- Goose: per spike result

### D6 — Transport docs

`docs/transports.md` was drafted in PHASE-05 D10. Phase 07 polishes it with:
- Screenshots of Tailscale account setup
- Cloudflared install per OS
- iOS cert profile install for LAN

### D7 — 90-second screencast

Tool: QuickTime + iMovie (macOS); or `OBS` for multi-source.

Storyboard:
- 0:00 — Title card: "Onibi — pause, your phone buzzes, you guide."
- 0:05 — Mac desktop, Onibi app open, file tree visible, Claude Code tab idle.
- 0:10 — User types: `claude code "remove all the old test files"`
- 0:20 — Claude pauses at `Bash(rm -rf tests/legacy)`. Desktop tab flashes red.
- 0:22 — Phone (held up to camera) buzzes simultaneously. Lock screen shows approval notification.
- 0:30 — User unlocks phone, taps notification. ApprovalCard opens.
- 0:38 — User taps "Edit". Textarea opens. Changes `rm -rf tests/legacy` to `mv tests/legacy tests/legacy.bak`.
- 0:50 — Taps "Approve with edits". Phone confirms. Desktop modal closes.
- 0:55 — Claude resumes; terminal shows `mv tests/legacy tests/legacy.bak`.
- 1:05 — Claude completes the task; output shown.
- 1:15 — Cut to: list of supported agents (icons row), list of platforms (macOS / Linux / Pi), list of transports (Tailscale / Cloudflare / LAN).
- 1:25 — Title card: "Apache-2.0. github.com/gongahkia/onibi"
- 1:30 — End.

Render as `.mp4` (H.264, 1080p, 60fps) + `.gif` (10fps, 720p for README embed). Commit to `asset/screencast/`.

### D8 — Blog post draft

**File**: `docs/blog/2026-06-XX-launch.md` (replace XX with real launch date)

Title candidates:
- "Designing a Multi-Transport Approval Protocol for Local AI Agents"
- "How I Built a Phone-First Cockpit for Claude, Codex, and Friends"
- "Approving AI Agent Actions From Anywhere: A Protocol and a Toolkit"

~1500 words. Sections:
1. The problem: AI agents pause, you're not there, the train is late.
2. Why not just SMS? Why a protocol matters.
3. The pieces: Tauri shell, headless daemon, mobile PWA.
4. Multi-transport design (Tailscale + Cloudflared + LAN) and why each is included.
5. The Claude Code `updatedInput` hard dependency and why edit-then-approve is the whole point.
6. What's next (v1.6: per-device tokens, audit log; v2.0: hosted relay).
7. Code is Apache-2.0 — fork it, ship adapters, complain on GH.

Cross-post on personal blog + DEV.to + Lobste.rs + HN Show.

### D9 — Launch channels

**HN Show post** (`docs/launch/hn.md` for draft):
- Title: "Show HN: Onibi — Approve your local AI coding agent from your phone"
- Body: pitch (2 paragraphs) + link to README + GIF + repo URL.
- Best time: Tuesday or Wednesday US morning (per WORKON-PIVOT-ASAP.md).

**X (Twitter) thread** (`docs/launch/x-thread.md`):
- 8-10 tweets; demo GIF in first; arch diagram in second; install one-liner in last.

**Lobste.rs**: link submission with "show" tag.

**Reddit**: r/programming, r/MachineLearning if relevant.

**DEV.to**: cross-post of blog.

**Mastodon**: a short thread mirroring X.

Coordinate so they fire within 30 minutes; HN post is the lead.

### D10 — Final QA pass

Run the full test matrix one last time before tagging:
- All Rust tests green
- All frontend tests green
- All three transports demonstrably working
- Fresh-machine dry-runs ≤5 min still holding
- `cargo audit`, `pnpm audit` clean
- README install one-liner actually works for a friend who's never seen the repo

### D11 — Tag and release

```
git checkout v1.5
git merge --no-ff v1.5/phase-07-launch
git tag -a v1.5.0 -m "Onibi v1.5.0 — Tauri cockpit + mobile second-screen"
git push origin v1.5 v1.5.0
```

GitHub Actions release workflow auto-builds and uploads binaries. Manually craft the GH Release notes from the blog post body.

Merge `v1.5` → `main` after the launch dust settles (Day +1).

### D12 — Post-launch monitoring

For the first 48 hours:
- Watch GH issues + HN thread for show-stoppers.
- Have a hotfix branch ready: `v1.5.1-hotfix`.
- Triage criteria: only ship a hotfix for crashes, security issues, or onboarding blockers. Everything else → v1.6.

## Exit criteria

1. README rewritten, screencast embedded, install one-liners verified.
2. Blog post published.
3. HN Show post live (Tuesday or Wednesday US morning).
4. X thread fires within 30 min of HN post.
5. v1.5.0 tag exists; release binaries available for all four targets.
6. `cargo audit` and `pnpm audit` clean.
7. `onibi doctor` ships and runs on all three platforms.
8. `docs/security.md`, `docs/adapters.md`, `docs/transports.md`, `docs/architecture.png` all present.
9. No P0 (crash, data loss, security) issue open at launch hour.

## Scope-cut order if behind

Per the v1 plan, preserve the killer demo last. Cut in this order:
1. Drop Goose adapter (no behaviour change at the protocol layer; remove from tabs/UI).
2. Drop Aider adapter.
3. Drop Cloudflared transport (Tailscale + LAN still cover the cross-network story).
4. Drop the file-tree + editor pane (terminal-only mode).
5. Drop Pi headless build (v1.6 follow-up; macOS + Linux is enough for HN).

Stop cutting when timeline fits. **Never cut PHASE-04 (mobile + push) — that's the moat.**

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git checkout v1.5
cargo audit
cd app && pnpm audit
# Final dry-runs:
cd .. && ./scripts/dry-run-fresh-machine.sh macos
./scripts/dry-run-fresh-machine.sh ubuntu
./scripts/dry-run-fresh-machine.sh pi
# Tag:
git tag -a v1.5.0 -m "Onibi v1.5.0"
git push origin v1.5.0
```

## Reference reading

- All previous PHASE docs.
- `docs/archive/WORKON-PIVOT-ASAP.md` Launch artefacts checklist (mostly carried forward).
- [HN Show submission guidelines](https://news.ycombinator.com/showhn.html)

## PR template

Title: `phase-07: hardening, docs, launch`

Body:
```
## Deliverables
- [x] D1 docs/security.md (threat model, hardening checklist done)
- [x] D2 onibi doctor command
- [x] D3 docs/architecture.png + source
- [x] D4 README final rewrite
- [x] D5 docs/adapters.md
- [x] D6 docs/transports.md polish
- [x] D7 screencast (mp4 + gif in asset/screencast/)
- [x] D8 docs/blog/2026-06-XX-launch.md draft
- [x] D9 launch channel drafts (HN, X, Lobste.rs, DEV, Reddit, Mastodon)
- [x] D10 final QA matrix
- [x] D11 v1.5.0 tag pushed
- [x] D12 hotfix readiness

## Verification
- [x] cargo audit clean
- [x] pnpm audit clean
- [x] dry-run x3 ≤5min
- [x] README install verified by external friend
- [x] release binaries downloadable

Onibi v1.5.0 ships when this is merged.
```
