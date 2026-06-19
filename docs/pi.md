# Kelp Pi — Hardened Field AppSec Drop-Box

## Status

This is the upstream design doc for the Pi-native data plane inside KelpClaw. The
work was scoped in a now-retired `bloob/` staging directory and absorbed into this
repo on 2026-06-19. Companion task list lives at [`pi-todo.md`](./pi-todo.md). The
product is **KelpClaw**; the Pi-specific surface is **Kelp Pi**; the agent binary
is **kelp-pi-agent**; the CLI subcommand family is `kelp-claw pi ...`.

Implementation has not started. P0 in [`pi-todo.md`](./pi-todo.md) is the next set
of work. Treat the design here as the source of truth; if reality diverges as code
lands, update this file rather than letting it rot.

## One-line pitch

A reproducible AppSec triage device you carry. Signed audit bundles, policy-gated
scanning, offline retrieval with citations, no cloud dependency, no exploit execution
by default.

## Concept

KelpClaw already produces reproducible AppSec triage on a laptop: scoped Docker target
build, passive scanner imports (SARIF, Nuclei, ZAP, Nmap, Burp, Nessus), policy-gated
agent triage under `appsec-agent-baseline`, normalized findings, signed audit bundles
verifiable without trusting any chat transcript.

Kelp Pi extends that same product to a battery-powered Raspberry Pi 5 you bring to an
engagement. The Pi runs scanners on-site within an operator-declared scope, ingests
their output as kelp evidence, runs policy-gated triage, signs a bundle locally, and
exposes a local `/ask` endpoint that retrieves over the engagement's docs, evidence,
and prior bundles. When connectivity returns, the Pi syncs signed bundles to the
control-plane API. Offline operation is the default.

## Operator persona

Primary: security researcher / red-teamer on airgap or hostile network. CLI-comfortable,
trusts no network they did not bring, wants reproducibility and audit, will hand a
signed bundle to a client without any chat transcript attached.

Secondary: pentest consultancy delivering a client report. The signed bundle becomes
the deliverable.

Out of scope as a primary persona: journalists, NGOs, hobbyists looking for an
offline knowledge box. They are welcome users but the product is not designed for them.

## Why this exists vs. prior art

The combination is novel. Each ingredient exists separately:

- `xPrep`, `Project NOMAD`, `Internet-in-a-Box`, `Kiwix` — offline knowledge boxes on Pi,
  but no audit discipline, no scanner integration, no signed evidence.
- `pi-local-rag`, `AIngram`, `brain.md`, `Deployable-Knowledge` — local-first RAG on
  SQLite + FTS5, but no AppSec evidence model, no policy gates, no Pi field-ops.
- Commercial pentest dropboxes (Hak5, Pwnagotchi, similar) — hardware-focused, no
  reproducibility or signed evidence story, often optimized for covert use which is
  explicitly out of scope here.

Kelp Pi's wedge: every action the Pi takes is policy-evaluated, every artifact is
signed, every answer is cited, and the whole engagement reconstructs deterministically
from the resulting bundle.

## Architecture: control plane + data plane

```
laptop / server                              raspberry pi 5 (field)
+-------------------------+                  +----------------------------+
| kelp control plane      |                  | kelp-pi-agent (Rust)       |
| - packages/cli (TS)     | <== signed ====> | - scanner runners          |
| - packages/evidence     |   bundles via    | - sqlite + FTS5 + vec      |
| - packages/policy       |   ssh / lan      | - policy gate (subset)     |
| - packages/nanoclaw     |   sync           | - signed local audit       |
| - api (Fastify)         |                  | - /ask retrieval endpoint  |
| - audit bundle verify   |                  | - systemd service          |
+-------------------------+                  +----------------------------+
```

Control plane stays in TypeScript under the existing kelp monorepo, unchanged in
shape. New work happens in `packages/pi-agent` (Rust) plus a thin `packages/pi-cli`
(TS) that exposes the `kelp-claw pi ...` subcommand family from the existing CLI.

The Pi agent is a single statically-linked binary (~10MB target) cross-compiled to
`aarch64-unknown-linux-gnu`. It is the only daemon required on the Pi.

### Wire protocol

JSONL over TCP/TLS or local Unix socket. Every envelope is signed by the originating
side using an Ed25519 key. Envelope shape:

```
{ "msg_id": "...", "ts": "...", "sender": "pi|cp", "kind": "...", "payload": {...}, "sig": "..." }
```

Defined kinds (initial):

- `hello`, `welcome` — handshake with capability list and key fingerprints.
- `policy.pull`, `policy.push` — control plane delivers signed policy packs.
- `scope.set` — operator-declared engagement scope (CIDR/host allowlist, time window).
- `scan.request`, `scan.event`, `scan.complete` — scanner job lifecycle.
- `evidence.append` — scanner output normalized to kelp evidence schema.
- `bundle.export`, `bundle.fetch` — signed audit bundle transfer.
- `ask.query`, `ask.result` — RAG query and cited response.
- `selfcheck.run`, `selfcheck.report` — Pi health and posture report.

All kinds map to existing kelp packages where possible. New kinds (`scope.set`,
`selfcheck.*`, the bundle transfer pair) are additions to kelp's surface.

## Pi target hardware

- **Minimum**: Raspberry Pi 5, 8GB RAM, 64GB A2 microSD, active cooler, 20Ah USB-C PD bank.
- **Recommended**: Raspberry Pi 5, 16GB RAM, 256GB NVMe via PCIe HAT, active cooler,
  20Ah USB-C PD bank, rugged case.
- 4GB Pi 5 is not supported because ZAP's JVM heap and scan state must share memory
  with Nuclei/Nmap jobs, SQLite FTS5 retrieval, audit signing, and bundle staging.
  The 8GB floor leaves headroom for concurrent scanner + retrieval work without
  relying on swap; 16GB is reserved for ZAP-heavy or optional local synthesis profiles.
- Optional: e-ink status panel (v2), USB-Ethernet adapter for wired scope, GPS HAT for
  signed location attestation (v2).

## Threat model

Two axes are in scope: **network perimeter discipline** and **audit & forensics**.

Network perimeter discipline:

- The Pi MUST NOT initiate outbound connections except to control-plane endpoints on
  an explicit allowlist.
- The Pi's AP MUST isolate clients from each other and from the Pi's loopback services.
- The self-check command MUST refuse non-local targets and MUST flag any unexpected
  listening port.
- DNS sinkhole intercepts captive-portal connectivity check domains and returns the
  Pi's portal IP only; no upstream DNS resolution by default.

Audit & forensics:

- Every scanner invocation, policy decision, and `/ask` query is appended to a
  tamper-evident hash-chained log.
- Every evidence import and triage output is signed with the Pi's Ed25519 key.
- Audit bundles produced on the Pi MUST verify under kelp's existing
  `verify-audit-bundle` without modification.
- Chunk IDs in the retrieval index are content-hashed and deterministic: identical
  corpus inputs produce identical IDs across rebuilds, on Pi or laptop.

Out of scope (explicit non-goals):

- Operator anonymity, covert use, evasion of defender detection.
- Tradecraft hardening beyond the threat model above.
- Cellular stack, SMS/call simulation, phone replacement.
- Always-listening voice assistant.
- Internet scanning, scanning outside operator-declared scope, exploit execution by
  default, persistence on targets, lateral movement, credential exfiltration.
- General self-hosting / app-store platform.
- Uncited generated answers.

## Scope (phased)

**P0** — scoping, repo absorption, control/data-plane contract definition, target
hardware confirmation, signing key bootstrap design.

**P1** — Rust agent skeleton: cross-compile, systemd service, single binary, structured
logging, hash-chained audit log, signing primitives, wire protocol scaffolding.

**P2** — Pi networking & hardening: AP mode via NetworkManager, client isolation,
DNS sinkhole, captive-portal redirection, outbound allowlist via nftables, self-check.

**P3** — Retrieval foundation: ingest pipeline (markdown, text, PDF-derived text),
deterministic chunker, SQLite FTS5 index, `/ask` endpoint, citation format,
no-answer threshold, single-file CLI client.

**P4** — Scanner integration on Pi: Nuclei (Go binary, embedded), Nmap (system), ZAP
(opt-in, 16GB only), scope-gated invocation, rate limiting, output normalization to
kelp evidence schema.

**P5** — Policy gates on Pi: port a subset of `appsec-agent-baseline` decisions into
the Rust agent (deny destructive shell, deny exfil, require approval for active scans
past discovery), policy pack signed-delivery from control plane.

**P6** — Audit bundle parity: Pi produces audit bundles binary-identical in structure
to laptop-produced ones; `kelp-claw verify-audit-bundle` verifies them unchanged.

**P7** — Control plane sync: signed bundle push/pull, scope pull, policy pull, all
over the wire protocol; offline-tolerant with replay-after-reconnect.

**P8** — Optional LLM synthesis: Ollama on Pi for retrieval-then-generate with strict
no-answer behavior; gated behind a flag; never default.

**P9** — Eval & determinism: gold Q/A regression set, reproducible chunk-ID test,
audit bundle replay, scanner output stability harness.

**P10** — Field ops: battery monitoring, thermal monitoring, storage quota, recovery
mode, read-only root option, signed firmware-update path.

**P11** — Docs, demo engagement walkthrough, sample vulnerable target, launch posture.

## Repo migration plan

Status: scoping and absorption are complete (2026-06-19). Remaining items below.

1. ~~Land this IDEA + TODO under `bloob/` as scoping.~~ Done.
2. ~~Migrate IDEA/TODO into `kelp/docs/pi.md` and `kelp/docs/pi-todo.md`.~~ Done.
3. ~~Adopt project CLAUDE.md conventions in `kelp/`.~~ Done (`kelp/CLAUDE.md`).
4. Open a `pi-agent` branch on `kelp/`.
5. Create `packages/pi-agent` (Rust) and `packages/pi-cli` (TS) skeletons.
6. Add `kelp-claw pi` parent subcommand stub in `packages/cli`.
7. Update kelp top-level `README.md` to mention the Pi target.
8. Update `docs/architecture.mmd` to show the control-plane / Pi-data-plane split.

Steps 4–8 are tracked as concrete tasks in [`pi-todo.md`](./pi-todo.md) under P0.

## Acceptance checks (product-level)

- Operator can flash a Pi image, declare a scope, and produce a signed audit bundle
  in under 30 minutes from cold.
- Bundle verifies under `kelp-claw verify-audit-bundle` without Pi-specific flags.
- Pi `/ask` returns cited chunks offline; unknown queries return a no-answer response.
- Pi refuses any scanner action outside the declared scope and logs the refusal to the
  hash-chained audit log.
- Pi self-check refuses non-local targets and flags any unexpected exposed port.
- Rebuilding the retrieval index from the same corpus on the Pi and on a laptop
  produces identical chunk IDs.
- Pi survives loss-of-power mid-scan without corrupting the audit log or evidence
  workspace; on next boot, the run is marked interrupted and resumable.

## Research references

Existing prior art (for honest framing in the eventual README):

- xPrep, Project NOMAD, Internet-in-a-Box, Kiwix — adjacent offline-knowledge boxes.
- pi-local-rag, AIngram, brain.md — adjacent local-first RAG.
- Hak5 Bash Bunny, LAN Turtle, Pwnagotchi — adjacent commercial drop-boxes with
  different threat models.

Technical:

- KelpClaw existing surface: [`../README.md`](../README.md),
  [`appsec-harness.md`](./appsec-harness.md), [`deployment.md`](./deployment.md),
  [`product-hardening-roadmap.md`](./product-hardening-roadmap.md),
  `packages/{evidence,policy,nanoclaw,cli,agent-hooks,workflow-spec}`.
- SQLite FTS5 documentation, sqlite-vec extension.
- Raspberry Pi 5 specs, Pi OS 64-bit, NetworkManager AP mode notes.
- Nuclei templates, Nmap scripting, Ollama ARM64.
- Ed25519 signing primitives, hash-chain append-only log patterns.
- Citation hallucination research (FACTUM 2026) and no-answer pattern.

## Naming

The product is **KelpClaw**. The Pi-specific surface is **Kelp Pi**. The agent binary
is **kelp-pi-agent**. The CLI subcommand family is `kelp-claw pi ...`. The "bloob"
name is retired as of 2026-06-19; do not reintroduce it.
