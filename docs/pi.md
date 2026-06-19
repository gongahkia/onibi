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

JSONL over SSH-tunneled stdio. Every envelope is signed by the originating side using
an Ed25519 key. Envelope shape:

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

### Transport decision

Primary v1 transport: **SSH-tunneled stdio**.

Rationale:

- It exposes no new listening TCP service on the Pi beyond the operator-managed SSH
  server.
- It works on hostile LANs where the operator can reach SSH but cannot rely on local
  DNS, service discovery, or inbound control-plane connectivity.
- It reuses existing operator SSH key management for session setup while keeping
  Kelp Pi's Ed25519 envelope signatures as the protocol trust boundary.
- It keeps the first Rust agent implementation small: line-delimited JSON on stdin
  and stdout, with stderr reserved for local diagnostics.

Deferred transports:

- TCP+TLS is deferred until the control plane needs always-on push without SSH. It
  needs certificate enrollment, listener hardening, and a larger exposed surface.
- A Unix socket is allowed only as a local on-Pi daemon control path, not as the v1
  laptop-to-Pi transport.

Reference client implementation:

- `kelp-claw pi connect --host <pi>` starts
  `ssh kelp-pi@<pi> kelp-pi-agent wire --stdio`.
- The TS client writes one canonical JSON envelope per line to child stdin and reads
  one envelope per line from child stdout.
- The TS client validates `envelope.schema.json`, verifies the envelope signature
  against the current trust list, dispatches by `kind`, and records the raw line hash
  into the local audit trail.
- SSH exit status, stderr, and timeout are mapped to transport errors, not protocol
  payloads.

Reference server implementation:

- `kelp-pi-agent wire --stdio` runs as the SSH forced command or explicit remote
  command.
- The Rust server reads newline-delimited JSON from stdin, validates
  `envelope.schema.json`, verifies the control-plane signature, applies policy/scope
  checks, and writes signed response envelopes to stdout.
- Protocol data is stdout-only; stderr is reserved for local operator diagnostics
  before the protocol is established.
- The server exits non-zero on malformed JSON, schema failure, signature failure, or
  revoked key use after writing an audit-log refusal entry when possible.

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

## Data directory layout

Root: `/var/lib/kelp-pi`, owned by the dedicated `kelp-pi` user.

| Path                        | Purpose                                                                             |
| --------------------------- | ----------------------------------------------------------------------------------- |
| `/var/lib/kelp-pi/corpus`   | Operator-provided docs, scanner sidecars, and prior bundle text used for retrieval. |
| `/var/lib/kelp-pi/evidence` | Normalized scanner evidence before bundle export.                                   |
| `/var/lib/kelp-pi/bundles`  | Completed audit bundles staged for fetch or sync.                                   |
| `/var/lib/kelp-pi/index`    | SQLite FTS5 index, chunk metadata, and ingest state.                                |
| `/var/lib/kelp-pi/audit`    | Hash-chained audit-log segments and rotation manifests.                             |
| `/var/lib/kelp-pi/keys`     | Encrypted Pi private key files and public key metadata.                             |
| `/var/lib/kelp-pi/policy`   | Last-good signed policy packs and trust lists.                                      |
| `/var/lib/kelp-pi/scope`    | Active and historical signed engagement scopes.                                     |

Startup preflight:

- The root and every required child path must exist and be directories.
- Any world-writable required path is a hard startup failure.
- `kelp-pi-agent start --data-dir <path> --check-only` runs the same preflight used
  before daemon startup.

## Quota defaults

Defaults target the 64GB floor while leaving room for OS, evidence workspaces, and
audit bundles. NVMe reference units can raise these values through config.

| Scope     | Default | Applies to                                                                 |
| --------- | ------: | -------------------------------------------------------------------------- |
| Corpus    |  20 GiB | `/var/lib/kelp-pi/corpus` source files and derived text sidecars.          |
| Uploads   |   8 GiB | Pending operator uploads staged under `/var/lib/kelp-pi/evidence/uploads`. |
| Index     |   8 GiB | `/var/lib/kelp-pi/index` SQLite, FTS5, and ingest metadata.                |
| Audit log |   1 GiB | `/var/lib/kelp-pi/audit` hash-chain segments before rotation/export.       |

Override mechanism:

- `/etc/kelp-pi/agent.json` may define `quotas.corpus_bytes`,
  `quotas.uploads_bytes`, `quotas.index_bytes`, and `quotas.audit_log_bytes`.
- Override values are byte counts and must be positive integers.
- Missing override keys keep the compiled defaults reported by
  `kelp-pi-agent quota-defaults`.
- Lowering a quota below current usage refuses new writes for that scope; it does not
  delete existing evidence.
- The agent records effective quotas and a SHA-256 hash of the config file in the
  startup audit entry.

## OS image base

Reference image: **Raspberry Pi OS Lite 64-bit, Debian Trixie**. As of 2026-06-19,
the pinned baseline is the 2026-06-18 Lite image, kernel 6.18, Debian 13
(`trixie`), SHA256
`acff736ca7945e3b305f07cda4abdb870910e12634991da69783611756e381b3`.
Source: official Raspberry Pi OS downloads page.

Rationale:

- Raspberry Pi OS is the official supported OS for Pi hardware, and the 64-bit Lite
  image keeps the field unit headless and small.
- The image download page publishes release date, kernel version, Debian version,
  size, and SHA256, so flash artifacts can be pinned and re-verified.
- Major OS upgrades use fresh images only; no in-place Bookworm-to-Trixie upgrade is
  part of the supported field path.
- APT sources and `/etc/apt/preferences.d/kelp-pi` pin package origin and release to
  Raspberry Pi OS / Debian Trixie repos during image build; scanner binaries and Rust
  agent builds remain separately pinned by version or digest.
- Snap is not part of the reference image. `snapd` must remain absent unless a future
  task explicitly approves it, because snap auto-refresh and alternate package
  provenance complicate deterministic rebuilds.

Ubuntu Server ARM64 remains a compatibility target for later CI or non-Pi hosts, not
the reference field image.

Package pin and upgrade flow:

- The image build writes `/etc/kelp-pi/os-lock.json` with `image_release`,
  `image_sha256`, `debian_codename`, `kernel_release`, and a sorted `dpkg-query -W`
  package lock from the promoted staging unit.
- `/etc/apt/preferences.d/kelp-pi` pins `trixie` packages as the only allowed release
  and rejects accidental `bookworm`, `forky`, `testing`, or `unstable` pulls.
- The lock must include every installed package matching `raspberrypi-*`,
  `raspi-*`, `linux-image-*`, `linux-headers-*`, `network-manager`, `dnsmasq`,
  `nftables`, `openssh-*`, `nmap`, and `nuclei`; image builds fail if those package
  versions drift without a lock update.
- Routine field units run with those packages held. Operators do not run unattended
  upgrades on engagement devices.
- Manual upgrade procedure:
  1. Download the candidate Raspberry Pi OS Lite 64-bit image from the official
     Raspberry Pi OS download page and verify its SHA256 before flashing.
  2. Flash a staging unit, bootstrap `/var/lib/kelp-pi`, install `kelp-pi-agent`, and
     apply the candidate package lock.
  3. Reboot once, then run `kelp-pi-agent selfcheck --data-dir /var/lib/kelp-pi` and
     `kelp-pi-agent verify-audit-log --data-dir /var/lib/kelp-pi`.
  4. Run the scoped scanner/retrieval smoke on the fixture target and export a bundle.
  5. Promote the candidate only if selfcheck is clean, audit-log verification passes,
     the fixture bundle verifies unchanged on the laptop, and the new `os-lock.json`
     diff is reviewed.
  6. Rollback is a reflash to the previous pinned image plus the previous
     `os-lock.json`; no in-place downgrade is supported.

## Signing key custody

Decision: v1 uses an on-Pi Ed25519 private key stored as an encrypted file under
`/var/lib/kelp-pi/keys/`. The operator unlocks it with a passphrase during bootstrap
or daemon start; the agent keeps the decrypted key only in process memory.

Rejected custody options:

- HSM: rejects Kelp Pi's default offline posture and adds network dependency.
- OS keychain: weak fit for Raspberry Pi OS Lite headless operation and harder to
  reproduce across flash images.
- YubiKey: useful later for operator approval or control-plane signing, but v1 needs
  unattended envelope, audit-log, and bundle signing on the Pi. Ed25519 through
  YubiKey PIV also depends on newer token/PKCS#11 support.

File custody rules:

- Key file mode must be `0600`, parent directory mode must be `0700`, owner must be
  the dedicated `kelp-pi` user, and the agent refuses to start otherwise.
- Private key material is encrypted at rest with an Argon2id-derived passphrase key
  and AEAD; salt, KDF params, public key, key ID, creation time, and rotation counter
  are stored beside the ciphertext.
- Public keys are non-secret and may be copied into audit bundles and control-plane
  trust lists.
- Control-plane trust lists can revoke a Pi key by key ID; revocation means later
  envelopes and bundles from that key are refused.

## Ed25519 key bootstrap

Bootstrap procedure:

1. Flash the pinned Raspberry Pi OS Lite image and create the dedicated `kelp-pi`
   user before the first agent start.
2. Create `/var/lib/kelp-pi/keys/` as `kelp-pi:kelp-pi` with mode `0700`.
3. Run `kelp-pi-agent keygen --key-dir /var/lib/kelp-pi/keys --label <device-id>`
   on the Pi console or over SSH. The command prompts for the operator passphrase
   twice and refuses empty passphrases.
4. `keygen` writes `pi-ed25519.key.json` with mode `0600` and
   `pi-ed25519.pub.json` with mode `0644`.
5. `pi-ed25519.key.json` contains schema version, key ID, creation timestamp,
   rotation counter, Argon2id params, salt, nonce, and AEAD ciphertext over the
   Ed25519 private key bytes.
6. `pi-ed25519.pub.json` contains schema version, key ID, algorithm, device label,
   public key, creation timestamp, and rotation counter.
7. Enroll the public key on the control plane with
   `kelp-claw pi trust add --public-key pi-ed25519.pub.json --device <device-id>`.
8. First daemon start unlocks the private key, emits a signed `hello` envelope with
   the key ID, and refuses all sync until the control plane returns a signed
   `welcome` that includes the same trusted key ID.

Rotation path:

1. Generate a replacement key with
   `kelp-pi-agent key rotate --key-dir /var/lib/kelp-pi/keys --reason <reason>`.
2. The agent signs a rotation statement containing old key ID, new key ID, reason,
   timestamp, and monotonic rotation counter with both the old and new keys when the
   old key is still available.
3. The control plane adds the new key as `trusted`, marks the old key as
   `retiring`, and accepts both during a bounded overlap window.
4. After the next successful bundle export or operator confirmation, the control
   plane marks the old key `revoked`; later envelopes from it fail verification.

Revocation mechanism:

- Trust state lives in a signed control-plane trust list keyed by Ed25519 key ID.
- Valid states are `trusted`, `retiring`, and `revoked`.
- A revocation record contains key ID, device ID, revocation timestamp, reason, and
  optional last-known-good envelope timestamp.
- The control plane refuses `revoked` keys immediately. The Pi consumes the same
  trust list on `policy.pull`; if its active key is revoked, it stops scanner,
  bundle, and sync work until re-bootstrap.
- Emergency rotation after suspected capture starts from a clean flashed image and a
  fresh `keygen`; the old key is never reused.

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

Retrieval chunking:

- Default chunking is heading-aware deterministic token windows: target 512 tokens,
  64-token overlap, stable whitespace tokenization, and no model-dependent tokenizer.
- Markdown chunking keeps the current heading path with each chunk and starts a new
  window at heading boundaries when possible before applying the 512/64 window.
- Plain text chunking is paragraph-aware first, then falls back to the same token
  window rule when a paragraph exceeds the target.
- PDF ingest uses external `pdftotext` sidecars only; sidecar text follows the plain
  text path and keeps a `derived_from: pdf` source marker.
- `/ask` returns `no_answer` when FTS5 finds no matching chunks or when the maximum
  higher-is-better `-bm25(...)` score is below the configurable threshold. Default
  threshold: `0.000001`.
- `/ask` responses include a top-level `citations` array. Each citation is
  `{path, heading_path, chunk_id, start_byte, end_byte}` and maps directly to a
  returned chunk.
- The Pi agent serves `/ask` with `kelp-pi-agent serve-ask`. Default bind is
  `127.0.0.1:8765`; non-loopback binds require `--allow-non-loopback`.
- `/ask` applies per-IP rate limiting and an in-flight request cap before retrieval.
  Defaults: `60` requests/minute per IP and `8` concurrent requests.
- Operators can query over LAN with the single-file Python client:
  `scripts/kelp-pi-ask "question" --pi pi.local`.
- The client supports `--json` for machine-readable scripting output.
- Ingest records source file `mtime`, size, and content hash in SQLite. Changed
  sources replace that file's chunk rows in one transaction; unchanged chunks keep
  stable content-derived chunk IDs.
- `kelp-pi-agent selfcheck` reports `stale_index: true` when a regular file under
  `/var/lib/kelp-pi/corpus` has no matching `source_files` index row.
- The Rust agent recognizes the `appsec-agent-baseline` action vocabulary
  (`allow`, `deny`, `require-approval`, `log-only`) and all v1 rule IDs from the
  TypeScript policy pack.
- `policy.push` accepts only `cp` envelopes whose Ed25519 signature verifies against
  a trusted control-plane key, verifies the embedded policy hash, and persists the
  accepted pack under `/var/lib/kelp-pi/policy/current-policy.json`.
- Local policy evaluation covers scanner invocation, file operation, and outbound
  network gates; `evaluate_and_audit_local_policy` emits a hash-chained
  `policy-decision` audit event for each decision.

Physical capture assumptions:

- Powered-off capture model: attacker must recover the operator passphrase or defeat
  the at-rest encryption before using the Pi private key.
- Powered-on capture of an unlocked agent is a key-compromise event: the attacker may
  sign with that Pi key until the operator revokes it or the process stops.
- After suspected capture, the control plane must revoke the Pi key ID and distrust
  envelopes after the last operator-confirmed good timestamp.
- Kelp Pi does not claim tamper-resistant hardware custody in v1.

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
