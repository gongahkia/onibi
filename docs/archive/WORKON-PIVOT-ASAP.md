# WORKON-PIVOT-ASAP

Onibi v1 pivot plan. Authoritative scope, phasing, and roadmap. Supersedes `todo.md` for v1 work; current `todo.md` covers v0 (Ghostty notifier) which is being archived under tag `v0.x`.

## TL;DR

**Onibi v1 = phone-side cockpit for local AI coding agents.**

Watch what they're doing, approve/deny/edit when they pause for permission, across machines and across agents. OSS / Apache-2.0, no telemetry, no accounts, self-hosted over Tailscale Funnel.

Pitch line:
> Onibi — the ghost light for your local AI agents. They pause; you guide.

Goal: portfolio/signal artifact targeted at OSS / HN audience. 3 months full-time. Launch via HN Show + blog post on the approval-protocol design.

## Why this pivot

v0 (Ghostty terminal notifier with mobile remote-control) has:
- niche TAM (Ghostty-only, macOS-only)
- commoditised notifier feature
- mobile remote-terminal is dominated by free alternatives (tmux+Tailscale+Blink/Termius)

v1 (multi-agent approval gate + run inbox) has:
- novel category — nobody owns "second-screen for AI coding agents" yet; explicit aim is to name and own the category in the launch post
- moat against single-vendor platform risk (Anthropic, OpenAI cannot subsume cross-agent UX)
- demo-able in 90 seconds with visceral payoff
- reuses ~50% of v0's IP (gateway, realtime codec, detectors, shell hook installer)

## Non-goals (v1)

- no accounts, no SaaS, no billing
- no hosted relay (deferred to v2)
- no native iOS app (PWA only)
- no Windows host (deferred indefinitely)
- no audit log UI (deferred to v1.1; event log on disk exists as side-effect of event-sourcing)
- no team/multi-user features
- no telemetry, opt-in or otherwise

## Locked decisions

| Area | Decision |
|---|---|
| Goal | Portfolio/signal |
| Audience | OSS / HN / general dev |
| Time budget | full-time, ~12 weeks |
| Framing | Active control (approve from phone) — headline; passive run feed — supporting |
| Stack | Go daemon + Swift menubar shell + React/Vite PWA |
| Transport | Tailscale Funnel (single recommended path); LAN for dev |
| License | Apache-2.0 |
| Telemetry | none |
| Repo | pivot in place — tag `v0.x`, branch `v1`, merge to `main` on launch |
| Name | keep `Onibi` |
| Code reuse | reference, not reuse — port detectors/classifier to Go on-demand |
| Audit log UI | defer to v1.1 (event log on disk exists from day one) |

Why Go over Rust: faster iteration (~1–3 weeks of headroom over a 12-week budget), sweet-spot fit for HTTP + WS + JSON + sqlite + VAPID push, trivial cross-compile to Pi (`GOOS=linux GOARCH=arm64`), bigger contributor pool for adapter PRs. Swift menubar already covers the OS-native-systems signal; Rust on the daemon would be signal-redundant against the real artefact, which is `SPEC.md`. v2.0 hosted-relay rewrite in Rust stays open if commercial path activates.

## Support basin

### Hosts

| Host | Phase | Notes |
|---|---|---|
| macOS 14+ (x86_64, arm64) | Phase 4 | menubar shell + daemon |
| Linux x86_64 | Phase 4 | daemon + CLI; SystemD user unit |
| Raspberry Pi (aarch64) | Phase 4 | Pi 4 / Pi 5 64-bit OS; `GOOS=linux GOARCH=arm64 go build` |
| Raspberry Pi (armv7) | v1.1 | older Pis; `GOOS=linux GOARCH=arm GOARM=7 go build` |
| Windows | not planned | hooks story messy; revisit on demand |

Pi support is a deliberate signal choice. Demo: "self-hosted AI lab on a Pi 5, controlled from phone, approved from anywhere." Plays beautifully with Tailscale.

### Agents

| Agent | Phase | Hook surface | Notes |
|---|---|---|---|
| Claude Code | Phase 1 | `PreToolUse` HTTP hook | full approve/deny/edit via `updatedInput`; 10-min default timeout; lead adapter |
| Codex CLI (OpenAI) | Phase 3 | `~/.codex/hooks.json` | same JSON-stdin/stdout model; shell tool only — no `apply_patch` or MCP intercept [Unverified as of 2026-05] |
| opencode (sst/opencode) | Phase 3 | TBD | research spike required Phase 3 entry — [Unverified] hook surface |
| Aider | v1.1 | `onToolCalled` etc. | AiderDesk path also viable [Unverified for native Aider] |
| Gemini CLI | v1.1 | TBD | research spike before commit |
| Cursor agent CLI | v1.2 | TBD | only if Cursor ships a stable hook surface |

Adapter pattern lives in `internal/adapters/` Go package; community PRs welcome for new agents.

Hook-surface findings verified by WebSearch on 2026-05-15:
- Claude Code: HTTP hooks shipped Jan 2026; `PreToolUse` returns `permissionDecision: allow|deny|ask|defer`; **command mutation via `updatedInput` since v2.0.10** — this is the hard dependency that makes "edit-then-approve" possible without a clumsy deny-and-suggest loop.
- Codex CLI: hooks at `~/.codex/hooks.json`, JSON-stdin/stdout, `permissionDecision: deny|allow`; reliably fires for `Bash` but **not** for `apply_patch` or most MCP tool calls (as of doc date).
- Aider: hook system exists (`onTaskCreated`, `onPromptFinished`, `onToolCalled`, +30 events); native vs AiderDesk path not fully disambiguated.

Re-verify before each adapter implementation phase — these APIs are <6 months old and moving.

## Architecture

```
                 Claude Code / Codex / opencode (host process)
                        │
                        │ HTTP PreToolUse hook  (blocks)
                        ▼
   ┌──────────────────────────────────────────────────────────┐
   │  onibid  (Go, net/http + coder/websocket)                │
   │                                                          │
   │   ┌─────────────┐   ┌──────────────┐   ┌──────────────┐ │
   │   │ adapters/   │   │ approval     │   │ realtime ws  │ │
   │   │  claude     │──►│ store        │──►│ + web push   │ │
   │   │  codex      │   │ (event log)  │   │ (VAPID)      │ │
   │   │  opencode   │   └──────────────┘   └──────────────┘ │
   │   └─────────────┘                                        │
   │                                                          │
   │   pairing token (keychain / secret-service / file)       │
   └──────────┬────────────────────────────────────┬──────────┘
              │ Unix socket                        │ HTTPS via Tailscale Funnel
              ▼                                    ▼
     ┌─────────────────┐                  ┌─────────────────────┐
     │ macOS menubar   │                  │ Phone PWA           │
     │ (Swift, ≤1k LoC)│                  │ (React + Vite)      │
     └─────────────────┘                  └─────────────────────┘
```

Module layout:

```
daemon/
  go.mod                     # module: github.com/gongahkia/onibi
  cmd/
    onibid/                  # daemon binary: HTTP server, store, ws, push
    onibi/                   # CLI binary: setup, status, run, adapter, token
  internal/
    protocol/                # shared types, JSON codec, version negotiation
    store/                   # event-sourced approval/run store
    realtime/                # ws hub, push fanout
    push/                    # VAPID web push
    adapters/                # per-agent adapter implementations + hook templates
    transport/               # Tailscale Funnel control, LAN bind helpers
    secret/                  # keychain (darwin) / secret-service (linux) / file fallback
```

Library picks (load-bearing):
- HTTP: `net/http` stdlib + `chi` for routing
- WebSocket: `github.com/coder/websocket` (context-aware, post-nhooyr fork)
- Web push: `github.com/SherClockHolmes/webpush-go` (VAPID)
- SQLite (if used for store indexing): `modernc.org/sqlite` (pure-Go, no CGo)
- Keychain: `github.com/keybase/go-keychain` (darwin); `github.com/zalando/go-keyring` (cross-platform fallback)
- QR generation: `github.com/skip2/go-qrcode`

Hard rule: no CGo dependencies. Single static binary per `GOOS`/`GOARCH` pair.

## Protocol (SPEC.md draft scope)

Wire protocol lives in `SPEC.md` at repo root. Versioned. Vendor-neutral so 3rd parties can write phone clients or adapters.

Headlines:
- `POST /v1/approval/request` — adapter pushes pending approval; blocks (long-poll) until daemon has a decision
- `GET /v1/approval/pending` — phone fetches inbox
- `POST /v1/approval/:id/decide` — phone submits decision (allow/deny/edit with `updatedInput`)
- `WS /v1/realtime` — push channel (run-started, run-output-tail, approval-pending, approval-resolved, run-completed)
- `POST /v1/run/event` — adapter posts run lifecycle events (start, tool-use, output-chunk, end)

Auth: single shared bearer token (Phase 1); per-device tokens with revocation (v1.1).
Wire format: JSON. Server-Sent Events as fallback for WS-hostile networks (v1.1).

**Machine-id is first-class from day 1.** Every approval, every run event, every websocket message carries a `machine_id` field even in single-machine v1. v1.2 multi-machine fan-in then becomes a config flip rather than a schema migration. Cheap now, painful to retrofit later.

**Hard dependency — Claude Code `updatedInput`:** The approve/deny/**edit** UX rests on Claude Code's PreToolUse hook returning `{permissionDecision: "allow", updatedInput: {...}}` to execute the tool with phone-edited parameters, shipped in Claude Code v2.0.10. Without this, "edit-then-approve" collapses to a clumsy deny-and-suggest loop that costs an extra LLM turn. Minimum supported Claude Code version is documented at adapter-install time; `onibi adapter install claude-code` refuses to install against older versions and prints the upgrade command.

## Phased plan

### Phase 0 — Demolition + Foundation (Weeks 1–2)

Deliverables:
- tag `v0.x` on current `main`
- branch `v1`; rewrite root README around new pitch; move v0 README to `docs/v0-README.md`
- delete: `Onibi/Services/GhosttyIPCClient.swift`, `Onibi/Services/GhosttyConfigParser.swift`, `Onibi/Services/GhosttyCliService.swift`, `Onibi/Services/CloudflaredService.swift`, `Onibi/Services/TailscaleServeService.swift` (replaced with Funnel-only docs), `OnibiSessionProxy/*` (proxy shim concept dies), `Onibi/Services/WebAssetServer.swift` if no longer needed, theme-sync code in `Onibi/Services/SessionManager.swift`
- keep (reference only): `OnibiCore/Services/AssistantClassifier.swift`, `Onibi/Services/EventDetectors.swift`, `Onibi/Services/FalsePositiveReducer.swift`, `Onibi/Services/ShellHookInstaller.swift`, `OnibiCore/Services/PairingTokenStore.swift`
- create `daemon/` Go module with `cmd/` and `internal/` layout above
- GitHub Actions: `go test ./...` + `go vet` + `staticcheck` on macos-latest + ubuntu-latest; cross-compile matrix for `darwin/{amd64,arm64}`, `linux/{amd64,arm64,arm}`
- `SPEC.md` v0 draft committed

Exit criteria: `go test ./...` green; CI green across the full cross-compile matrix; v0 README archived; pivot announced in repo description.

### Phase 1 — Protocol + Claude Code adapter (Weeks 3–5)

Deliverables:
- `SPEC.md` v1.0 finalised
- daemon: HTTP server, event-sourced approval store (append-only file `~/.config/onibi/events.log` + in-memory replay), pairing token storage (keychain on macOS, secret-service on Linux, encrypted file fallback)
- WebSocket `/v1/realtime` with auth handshake
- Claude Code adapter: HTTP `PreToolUse` hook template + `onibi adapter install claude-code` CLI command (writes to `~/.claude/settings.json`)
- CLI: `onibi setup`, `onibi status`, `onibi token rotate`, `onibi adapter list|install|uninstall`
- integration test: spawn daemon, simulate Claude Code hook POST, decide via HTTP, assert hook returns `updatedInput`

Exit criteria: from a fresh Claude Code run, a `Bash(echo hi)` tool call blocks on the daemon, `curl` to decide unblocks it, hook returns `permissionDecision:allow`.

### Phase 2 — Phone PWA + Web Push (Weeks 6–7) — KILLER DEMO MILESTONE

Deliverables:
- strip current `OnibiWeb/` to bare bones, rebuild around approval inbox
- views: `InboxView`, `PendingApprovalCard`, `EditCommandView`, `PairingView`, `SettingsView`
- approval card: agent name, tool, command (monospace), cwd, last 50 lines of run output, three actions
- edit screen: monospace textarea pre-filled, diff highlight vs proposed, "approve with edits" button
- VAPID web push: keypair generated by daemon on first run, public key in pairing payload, subscription stored per-device
- iOS add-to-home-screen prompt + onboarding screen explaining push limitation
- pairing flow: QR + deep link `onibi://pair?host=...&token=...`

Exit criteria: **the demo works end-to-end**. Claude Code on Mac, phone in pocket, runs `claude code "delete node_modules"`, phone buzzes, tap approve, command runs. Screencast-able.

### Phase 3 — Multi-agent + run feed (Weeks 8–9)

Deliverables:
- Codex CLI adapter: writes `~/.codex/hooks.json`, hook script identical shape to Claude Code's
- opencode adapter: research spike (1 day) → implement; if hook surface is missing, file an upstream issue and stub adapter
- run lifecycle events wired: `run-started`, `tool-use`, `output-chunk`, `run-completed` with exit code
- inbox view extended: pending-approvals tab + recent-runs tab
- run detail view: full output, tool-use timeline, exit status
- agent identity: `{agent}/{cwd-basename}` with stable color per run-id

Exit criteria: 3 concurrent runs (1 Claude Code, 1 Codex, 1 opencode) visible on phone with distinct identity; at least 2 of 3 support approval interception.

### Phase 4 — macOS menubar + Linux/Pi packaging (Week 10)

Deliverables:
- Swift menubar app (~500–1000 LoC): status dot (green/yellow/red), recent approvals dropdown, "Open Inbox" → opens PWA, Settings → token rotation, Tailscale Funnel toggle, hook install/uninstall buttons
- daemon ↔ menubar IPC over Unix socket at `~/.config/onibi/control.sock`
- macOS: build a signed-but-unnotarised `.dmg` (notarisation deferred unless trivially scriptable)
- Linux: SystemD user unit `~/.config/systemd/user/onibi.service`; install script
- Raspberry Pi: cross-compiled `linux/arm64` binary in releases; install script detects ARM and pulls the right asset
- Homebrew tap: `brew tap gongahkia/onibi; brew install onibi`

Exit criteria: clean install on each of {macOS, Ubuntu 22.04, Pi 5 with Pi OS 64-bit}. `onibi setup` works on all three. **Onboarding definition-of-done: time from `brew install onibi` (or curl-script equivalent on Linux/Pi) to the first phone-approved Claude Code action is under 5 minutes on a fresh machine, with no docs read.** Timed dry-run on a fresh user account before exiting Phase 4.

### Phase 5 — Hardening + Launch (Weeks 11–12)

Deliverables:
- Tailscale Funnel setup doc — single recommended path, with screenshots
- security doc rewrite (port from `docs/security.md`, update for v1 model)
- architecture diagram (redraw `asset/reference/architecture.png`)
- 90-second screencast — agent pause, phone buzz, approve, agent finishes
- blog post draft: "Designing the local-AI-agent approval protocol"
- README final pass with embedded screencast, install one-liner, architecture diagram, support matrix
- HN Show post + X thread drafted

Launch window: Tuesday–Wednesday US morning. Timing flex if Anthropic / OpenAI ship something competing in the same week.

Scope-cut order if behind:
1. drop menubar shell (ship daemon + PWA + CLI; ship Swift app in v1.0.1)
2. drop opencode adapter (Phase 3 → 2 agents)
3. drop run feed (Phase 3 → approvals + live-tail only)

The killer demo (Phase 2) is preserved last.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Claude Code hook re-entrancy with user's own hooks | medium | medium | SPEC.md documents composition; daemon writes its hook last so it can defer to others |
| Codex `apply_patch` not interceptable [Unverified as of 2026-05] | high | low | demo with Codex uses `Bash` only; document limitation |
| opencode hook surface insufficient | medium | low | Phase 3 spike before commit; stub adapter if missing |
| iOS web push requires PWA home-screen install | certain | medium | onboarding screen; SMS-bridge considered for v1.1 |
| Tailscale Funnel quota / availability changes | low | high | second supported transport (LAN) documented; fallback path stays buildable |
| Swift ↔ Go IPC fragility | low | medium | Unix socket with versioned JSON protocol; menubar app can run without daemon (degraded) |
| 12-week timeline slip | medium | medium | staged scope-cut order above; killer demo preserved |
| Anthropic ships native phone approval UX | low | high | multi-agent + cross-machine is the moat; reposition if happens |
| Go + Swift bifurcation grows in cost | medium | low | hard cap: menubar ≤ 1k LoC, no Go package > 5k LoC, refuse feature creep into either |
| Claude Code `updatedInput` deprecated or changed | low | high | pin minimum supported version in adapter; SPEC.md negotiates protocol+host version on hook handshake; integration test breaks loudly on shape drift |

## Launch artefacts checklist

Roll-up of the assets that must exist on launch day. If any is missing, launch slips by a day, not by a feature.

- [ ] `SPEC.md` v1.0 — the senior-eng-legible artefact
- [ ] `README.md` rewritten around the v1 pitch with embedded screencast and install one-liner
- [ ] Architecture diagram (replace `asset/reference/architecture.png`)
- [ ] 90-second screencast — agent pause → phone buzz → tap approve → agent finishes
- [ ] Blog post draft — "Designing the local-AI-agent approval protocol"
- [ ] Security doc — threat model, auth caveats, hardening
- [ ] Tailscale Funnel setup doc with screenshots
- [ ] Homebrew tap published (`gongahkia/homebrew-onibi`)
- [ ] curl-pipe install script for Linux/Pi
- [ ] GitHub release with cross-compiled binaries for `darwin/{amd64,arm64}` + `linux/{amd64,arm64}`
- [ ] HN Show post drafted
- [ ] X thread drafted
- [ ] Fresh-machine timed dry-run completed (≤5 min from install to first approval)

## Future roadmap

### v1.1 (~2 months post-launch)

- audit log UI (data already on disk from v1)
- Aider adapter
- Gemini CLI adapter
- per-device pairing tokens with revocation
- structured argv editor on phone (not just monospace textarea)
- armv7 Pi support
- SSE fallback for WS-hostile networks

### v1.2 (~5 months post-launch)

- multi-machine fan-in: one PWA, N daemons over one Tailscale tailnet
- approval policies: auto-approve `git status`-class commands, require approval for `rm -rf`-class
- Slack / Discord / ntfy push as alternatives to web push
- Cursor agent CLI adapter (if hook surface ships)
- Linux ARM (32-bit) packaging

### v2.0 (~12 months post-launch, optional commercial fork point)

- hosted relay service (paid, ~$3–5/mo) — for users without Tailscale
- native iOS app (better push UX; widget; Lock Screen Live Activity for pending approvals)
- multi-stakeholder approvals (team feature — requires N-of-M approval for a class of commands)
- run replay & forking
- approval-protocol-as-a-spec: publish spec separately, encourage other agent vendors to adopt natively

The v2.0 commercial path stays optional. If Onibi v1 hits well as a portfolio piece, the choice between "stay OSS hobby" and "spin out as a company" stays open. Architecture in v1 deliberately doesn't foreclose either.

## Open decisions to lock before Phase 0 starts

1. start date for Phase 0
2. handle existing v0 issues/PRs — close, transfer to `archive` repo, or leave
3. brew tap repo name (`homebrew-onibi`?)
4. confirm Tailscale Funnel personal-account quotas suffice for solo dev demo workload
5. decide whether to keep `OnibiCore` Swift package as a `v0-archive/` subdirectory or delete

## Reference: what survives from v0

| v0 artefact | v1 fate |
|---|---|
| `OnibiCore/Services/AssistantClassifier.swift` | port to Go in Phase 3 if needed for opencode/Aider parsing |
| `Onibi/Services/EventDetectors.swift` | port for run-feed Phase 3 |
| `Onibi/Services/FalsePositiveReducer.swift` | port if heuristic detection used for agents without hooks |
| `Onibi/Services/ShellHookInstaller.swift` | keep concept; rewrite in Go; covers fallback for hookless agents |
| `OnibiCore/Services/PairingTokenStore.swift` | reference for keychain access patterns |
| `OnibiCore/Services/RealtimeGatewayCodec.swift` | reference; rewrite cleaner in Go |
| `Onibi/Services/MobileGatewayService.swift` (45k LoC) | discard structure; reuse only the protocol shapes |
| `OnibiWeb/src/` | strip and rebuild around approval inbox |
| Ghostty-specific code | delete |
| Cloudflared / ngrok wizards | delete |
| `OnibiSessionProxy/` | delete |

## Working agreements

- Phase 0 is non-negotiably first. No coding in Go until v0 demolition is committed and CI is green on the trimmed repo. Resists temptation to "just start the daemon."
- SPEC.md is updated before each phase's implementation. Protocol-first, not code-first.
- README does not get touched after Phase 0 until Phase 5. No marketing copy churn mid-build.
- Scope-cut order is sacrosanct. When week 9 looks tight, drop opencode adapter before dropping the menubar — but only because that order preserves the killer demo and the Linux/Pi story together.
- No new features outside this doc enter v1. Anything that surfaces gets queued for v1.1 in this file.
- Size caps: no Go package exceeds ~5k LoC without splitting; Swift menubar app caps at 1k LoC. v0's 45k-LoC `MobileGatewayService.swift` is the anti-pattern this rule exists to prevent.
- No CGo. Pure-Go libraries only, so single-binary cross-compile to Pi stays one command.
- Hook surfaces re-verified at the start of each adapter phase. Claude Code, Codex, and opencode APIs are <6 months old; the agents table's [Unverified] flags must be re-resolved before each adapter is implemented, not after.
