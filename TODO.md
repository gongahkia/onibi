# todo.md — Pi-resident, local-only, post-guardrail pivot

Current status, 2026-06-28: this root file is a retained planning artifact. The live task source is `docs/pi-todo.md`. The Linux/aarch64 linked package and preflight path are implemented; the next gate is real Raspberry Pi 5 SSH acceptance evidence.

Status: retained planning artifact. Some lines below are historical and may already be implemented; use `docs/pi-todo.md` for current completion state.

## Immediate live Pi retry checklist

- [ ] Reconnect Pi 5 to the same network as the Mac and discover its current IP (`hostname -I` on Pi, router UI, or network scan).
- [ ] Verify passwordless SSH with dedicated key: `ssh -o BatchMode=yes -i ~/.ssh/kelp_pi_acceptance gongahkia@<pi-ip> true`.
- [ ] Confirm target facts: `uname -m`, `/proc/device-tree/model`, `df -h /tmp`, and `command -v rsync || true`.
- [ ] Install Pi `rsync` if missing: `sudo apt-get update && sudo apt-get install -y rsync`.
- [ ] Update `.kelp-pi/acceptance.env` with the current `KELP_PI_SSH_HOST=<pi-ip>`.
- [ ] Rerun `pnpm accept:pi`; if the network drops during GGUF transfer, rerun after reconnect because the harness now prefers resumable rsync.
- [ ] After pass, inspect `.kelp-pi/acceptance-evidence/{doctor.json,model-warm.json,bundle-verify.json}` and mark `docs/pi-todo.md` real Pi acceptance items complete.

Labels: `[Inference]` = derived from analysis. `[Speculation]` = uncertain, needs Pi-side verification. `[Unverified]` = claimed elsewhere, not yet measured.

---

## 0. North star

One sentence: **Fully-local AppSec triage chat agent for Raspberry Pi 5 — no API keys, no provider guardrails, no egress, no rate limits, signed reproducible evidence.**

Rationale to foreground in all docs: cloud-hosted agents are progressively guardrailed (user-cited examples: Fable, GPT-5.6 — record verbatim, do not paraphrase). On-device + owner-controlled weights is the only durable foundation for adversarial / red-team workflows on airgap.

Audience: red-teamer / security researcher on airgap. CLI-only. SSH-or-direct on the Pi. No laptop required to run.

---

## 1. Value prop reframe (docs)

### 1.1 `README.md` — rewrite

- Replace opening paragraph + hero block. New lead: post-guardrail framing, then reproducibility/audit-bundle pillars (existing strengths, do not lose).
- Hardware floor: **Pi 5 4GB SD-only** (was: 4GB minimum, 8GB+NVMe recommended). 8GB stays as "more headroom" not "recommended baseline".
- Quickstart: collapse "Pi" + "laptop" paths into one Pi-resident path. Laptop entirely optional.
- Provider table: delete. There is one model surface — local GGUF.
- Add named pillars list:
  1. Local-only, no third-party guardrails.
  2. Reproducible signed audit bundles.
  3. Policy-gated active scanning + per-action approval.
  4. Offline cited retrieval (SQLite FTS5).
  5. Single static binary, no daemon, no container.

### 1.2 `CHANGES.md` — append

- Entry: "Local-only pivot. TS control-plane retired. Rust pi-agent → Zig `kelp-pi`. Cloud SDKs removed. Chat REPL added."

### 1.3 `docs/pi.md` — surgical edits

- Operator persona: foreground "post-guardrail red-teamer on airgap" (memory already records this).
- Strip control/data-plane split language from architecture sections. There is no control plane at runtime.
- Keep: signing key custody, on-Pi Ed25519, data layout `/var/lib/kelp-pi`, OS baseline (Raspberry Pi OS Lite 64-bit), hardware floor.
- Update wire-protocol section: SSH-tunneled JSONL stays as **optional** evidence-pull surface for an external auditor, not as the agent transport.

### 1.4 `docs/appsec-harness.md` — rewrite

- Remove `--agent-command` external-subprocess assumption. The chat agent is **in-process** in the Zig binary.
- Rewrite I/O contract: input = chat turn + scanner imports + scope; output = signed transcript + findings + bundle.

### 1.5 `docs/architecture.mmd` — redraw

- Remove TS control-plane node entirely.
- Single Zig binary node: chat-REPL → triage-architect → scanner-editor → policy → scanner orchestration → evidence workspace → SQLite FTS5 → signed bundle.
- Local `llama.cpp` runtime as a sub-node inside the binary.

### 1.6 `docs/pi-quickstart.md` — rewrite

- Drop `kelp-claw pi bootstrap` (laptop-side).
- New flow: flash OS → `curl ... install-kelp-pi.sh | sudo sh` → `kelp-pi doctor` → `kelp-pi chat`.
- First-chat walkthrough using a fixture SARIF.

### 1.7 `docs/pi-prior-art.md` — extend

- Add Aider (architect/editor split, atomic-commit discipline, edit-format rigour).
- Add Cline (read→propose→approve→execute→read loop, granular per-action approvals, XML tool-call format).
- Add Continue.dev (open-source agent loop reference).
- Note explicitly which patterns are adopted vs. rejected (no YOLO mode — see §5.2).

### 1.8 `docs/pi-threat-model.md` — extend

- New section: "Why no YOLO / auto-approve-all mode". Document rationale: AppSec posture demands per-action accountability; mass-approval defeats the audit story.
- New section: "Local model failure modes". Hallucinated tool calls, refusal mimicry, prompt-injection from scanner output. Mitigation: policy gates run before any tool dispatch regardless of model output.

### 1.9 `docs/web-intel.md` — likely delete

- Airgap operator, no Internet by policy. Confirm during execution; if any non-network helper survives, retain. Otherwise move to `legacy/`.

---

## 2. Architecture collapse

### 2.1 Packages to retire from runtime (move to `legacy/`, do not delete)

Move-not-delete preserves audit history of the prior architecture, matching the prior Find-Evil cleanup pattern in `CHANGES.md`.

| Package                  | Disposition            | Reason                                         |
| ------------------------ | ---------------------- | ---------------------------------------------- |
| `packages/cli`           | `legacy/cli`           | Laptop CLI superseded by on-Pi `kelp-pi`       |
| `packages/pi-cli`        | `legacy/pi-cli`        | Laptop-side Pi control superseded              |
| `packages/nanoclaw`      | `legacy/nanoclaw`      | Cloud agent runner removed                     |
| `packages/codegen`       | `legacy/codegen`       | Cloud codegen removed                          |
| `packages/agent-hooks`   | `legacy/agent-hooks`   | Claude Code hook integration not needed on Pi  |
| `packages/adapters`      | `legacy/adapters`      | MCP adapter — see §11 open question            |
| `packages/web-intel`     | `legacy/web-intel`     | Airgap, no Internet                            |
| `packages/workflow-spec` | `legacy/workflow-spec` | TS schemas — port what survives to Zig structs |
| `packages/testing`       | `legacy/testing`       | TS test harness retired with TS code           |

### 2.2 Packages to port to Zig (reference until replaced)

| Source                                   | Target                                 | Notes                             |
| ---------------------------------------- | -------------------------------------- | --------------------------------- |
| `packages/policy/src/packs.ts` (rules)   | `policies/*.toml` + `app/policy/*.zig` | Declarative packs + Zig evaluator |
| `packages/evidence/src/*.ts` (importers) | `app/evidence/*.zig`                   | One module per scanner format     |
| `packages/pi-agent/src/*.rs`             | `app/*.zig`                            | Module-by-module port — see §3.2  |

### 2.3 New top-level layout

```
app/                 # Zig sources
  main.zig
  chat/              # REPL, architect/editor split, prompt assembly
  model/             # llama.cpp glue, tokenization, sampling
  policy/            # evaluator, expression parser
  scanner/           # nuclei, nmap, zap orchestration + sandbox
  evidence/          # sarif, nuclei, nmap, zap, burp, nessus importers
  bundle/            # signing, verification, audit chain
  retrieval/         # SQLite FTS5 wrapper, citations
  hardening/         # systemd, nftables, thermal, boot
build.zig
policies/            # *.toml policy packs
models/manifest.toml # pinned GGUF URLs + SHA256
vendor/              # llama.cpp submodule, nuclei aarch64, templates tarball
scripts/             # install, release (retained, retargeted)
legacy/              # archived TS + Rust tree, build excluded
```

### 2.4 Root files to delete (post-port)

- `pnpm-workspace.yaml`
- root `package.json`
- root `tsconfig.base.json`
- `Dockerfile.api`, `Dockerfile.kelp`, `Dockerfile.kelpclaw`
- `docker-compose.yml`

Keep `.env.example` but rewrite (see §6).

---

## 3. Language pivot: Rust → Zig

### 3.1 Toolchain

- **Recommendation: Zig.** Rationale: single-binary aarch64 cross-compile with no toolchain install, native `llama.cpp` C-ABI linkage, `-O ReleaseSmall -fstrip` produces a sub-10MB static binary [Inference], modern memory-safety story without Rust's borrow-check ceremony. [Inference based on web research, not measured.]
- Zig version: pin latest stable at execution time; record exact version in `build.zig` comment header.
- C++ fallback: only for `llama.cpp` itself (vendored as submodule, built via `build.zig`). No other C++ deps allowed.
- C fallback: only for narrow libc syscalls Zig stdlib doesn't yet expose.
- **Retire entirely**: `cargo`, `cross`, `cargo-zigbuild`.

### 3.2 Module port table

Single static binary `kelp-pi` (rename from `kelp-pi-agent`).

| Existing Rust (`packages/pi-agent/src/`) | Target Zig (`app/`)                                   | Priority | Notes                                                                                                    |
| ---------------------------------------- | ----------------------------------------------------- | -------- | -------------------------------------------------------------------------------------------------------- |
| `main.rs`                                | `main.zig`                                            | P0       | Subcommand dispatcher: `chat`, `doctor`, `keygen`, `scan`, `ask`, `bundle`, `policy`, `approve`, `scope` |
| `lib.rs`                                 | (split)                                               | P0       | Re-export points; collapse into module roots                                                             |
| `policy.rs`                              | `policy/evaluator.zig` + `policy/expressions.zig`     | P0       | Same action verbs (allow/deny/require-approval/log-only)                                                 |
| `scanner.rs`                             | `scanner/orchestrator.zig`                            | P0       | systemd-run + nftables marks + rate limits                                                               |
| `nuclei.rs`                              | `scanner/nuclei.zig`                                  | P0       | Version + template pin constants preserved                                                               |
| `nmap.rs`                                | `scanner/nmap.zig`                                    | P1       |                                                                                                          |
| `zap.rs`                                 | `scanner/zap.zig` + `hardening/pi5_probe.zig`         | P1       | Split: scanner vs `/proc/device-tree/model` hardware probe currently colocated here                      |
| `ollama.rs`                              | **delete** → `model/llamacpp.zig`                     | P0       | No Ollama daemon. Direct `llama.cpp` linkage. RAM gate logic preserved.                                  |
| `hardening.rs`                           | `hardening/network.zig` + `hardening/peripherals.zig` | P1       | nftables + AP mode + Bluetooth/audio/HDMI disable                                                        |
| `thermal.rs`                             | `hardening/thermal.zig`                               | P1       | Scan gate on temp threshold                                                                              |
| `boot.rs`                                | `hardening/boot.zig`                                  | P2       | `/boot/config.txt` mutations                                                                             |
| `bundle.rs`                              | `bundle/sign.zig` + `bundle/verify.zig`               | P0       | Ed25519 signing, audit chain                                                                             |
| `keys.rs`                                | `bundle/keys.zig`                                     | P0       | On-Pi keygen, never-export invariant                                                                     |
| `tests/*.rs`                             | `app/**/*_test.zig`                                   | P1       | Zig `std.testing`                                                                                        |

### 3.3 Cross-compile recipe (the only supported build path)

```
zig build -Doptimize=ReleaseSmall -Dtarget=aarch64-linux-gnu -Dstrip=true -Dsingle-threaded=false
```

Single output: `zig-out/bin/kelp-pi`.

### 3.4 Tests to port verbatim from `packages/pi-agent/tests/`

`policy_sync`, `ollama` (rename → `llamacpp_ram_gate`), `scan_scope`, `acceptance_manifest`, `storage_quota`. Each becomes a Zig integration test under `app/**/`.

---

## 4. Local model selection (4GB Pi 5 is the constraint)

### 4.1 Primary: **Qwen3 0.6B Q4_K_M** via `llama.cpp`

Rationale:

- Ties Qwen3-4B and Phi-4-mini at 0.880 on the BFCL tool-calling benchmark. `[Inference]` — based on the 2026 ertas.ai benchmark write-up, not measured locally.
- Q4_K_M weights ~400-500MB. KV cache headroom comfortable on 4GB after kernel + scanners + SQLite. `[Inference]`
- ARM NEON / dotprod / fp16 path on Pi 5 Cortex-A76 well-supported in `llama.cpp`.
- Expected throughput: 10-18 tok/s on Pi 5 for 1B-tier models per cited benchmarks. `[Unverified]` — must measure 0.6B specifically.

### 4.2 Secondary candidates (list in `models/manifest.toml`)

| Model           | Q      | Size est. | Notes                                                                         |
| --------------- | ------ | --------- | ----------------------------------------------------------------------------- |
| Llama 3.2 1B    | Q4_K_M | ~700MB    | Broader instruction-tuning, weaker tool-calling                               |
| Gemma 3 1B      | Q4_K_M | ~700MB    | Small, weaker reasoning chain                                                 |
| Phi-4-mini 3.8B | Q4_K_M | ~2.2GB    | **Must-measure on 4GB Pi** — may not leave KV-cache headroom. `[Speculation]` |
| Qwen3 4B        | Q4_K_M | ~2.4GB    | Likely OOM on 4GB. `[Speculation]`                                            |
| TinyLlama 1.1B  | Q4_0   | ~600MB    | Known-good baseline (14.4 tok/s cited). Fallback.                             |

### 4.3 Build flags for `llama.cpp`

```
-DGGML_NATIVE=ON
-DGGML_CPU_ARM_ARCH=armv8.2-a+dotprod+fp16
```

`[Inference]` — Pi 5 Cortex-A76 ISA level. Confirm during build configuration.

### 4.4 Model registry: `models/manifest.toml`

Schema:

```toml
[[model]]
id = "qwen3-0.6b-q4_k_m"
url = "https://huggingface.co/..."
sha256 = "..."
primary = true
ram_floor_mb = 1024
```

Installer pulls primary; doctor reports which models are present + their hashes.

### 4.5 Tradeoff vs. existing Pi P8 (Ollama) design

Existing `packages/pi-agent/src/ollama.rs` + `tests/ollama.rs` assume Ollama daemon. **Drop entirely.** Replace with direct `llama.cpp` static link. Single-binary story is incompatible with a daemon dep. Record this break in `CHANGES.md`.

---

## 5. Chat REPL UX (Aider + Cline patterns)

### 5.1 Surface

- Single subcommand: `kelp-pi chat` opens a long-running session.
- Session id is a ULID. State persisted to `/var/lib/kelp-pi/sessions/<id>/`:
  - `transcript.jsonl` (append-only, signed)
  - `state.json` (scope, scanner outputs, approvals)
  - `findings.json` (incremental)

### 5.2 Loop shape (Cline-derived)

```
read context → propose action → evaluate policy → request approval (if required) → execute → read output → repeat
```

Policy evaluation runs **before** approval, **always**, regardless of model output. Approval is the second gate.

Approval modes:

- **Default**: per-action prompt.
- `--auto allow:<class>[,<class>...]`: granular whitelist by action class (read, evidence-import, retrieval-query). **Never** allow `scanner-active`, `policy-mutation`, `bundle-sign`.
- **No YOLO / no `--auto allow:*`.** Document rejection rationale in `docs/pi-threat-model.md` (§1.8).

### 5.3 Architect/Editor split (Aider-derived)

Two prompts under the hood, one model:

- **Triage architect**: input = current finding + retrieval citations; output = plain-English analysis + proposed next evidence step.
- **Scanner editor**: input = architect's proposal; output = deterministic tool invocation (XML-tagged, see §5.4).

`[Inference]` — Aider's architect/editor split is documented for code edits; mapping to AppSec triage is novel. Validate on Pi.

### 5.4 Tool-call format: XML-tagged (Cline style)

Small models (0.6B-1B) follow tag structure more reliably than JSON-schema adherence. `[Inference]` — broadly observed; not directly cited.

Tag set (initial):

```
<scan tool="nuclei" target="<url>" scope-id="<id>" />
<import format="sarif" path="<path>" />
<ask query="<text>" top-k="5" />
<scope target="<host>" until="<rfc3339>" />
<approve token="<token>" />
<finish summary="<text>" />
```

Each tag maps 1:1 to an action class with a fixed policy gate.

### 5.5 Session persistence

- Append-only signed JSONL transcript at `/var/lib/kelp-pi/sessions/<id>/transcript.jsonl`.
- Hashed into the audit bundle on `kelp-pi bundle --run-id <id>`.
- No mutation, no deletion. Rotation policy: storage quota gate from existing `tests/storage_quota.rs` logic.

---

## 6. Cloud SDK excision

### 6.1 File-by-file

| File                                                       | Action                                                                                                                                                  |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `packages/nanoclaw/src/agentic-runner.ts`                  | Move to `legacy/nanoclaw/`                                                                                                                              |
| `packages/nanoclaw/package.json`                           | Move (whole package retired)                                                                                                                            |
| `packages/codegen/src/agent-sdk-generator.ts`              | Move to `legacy/codegen/`                                                                                                                               |
| `packages/codegen/src/openai-generator.ts`                 | Move to `legacy/codegen/`                                                                                                                               |
| `packages/codegen/src/openweight-generator.ts`             | Move to `legacy/codegen/` (despite "open" in name — surface is laptop-side, retired)                                                                    |
| `.env.example`                                             | Rewrite: strip `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `KELPCLAW_AGENTIC_PROVIDER`. Add `KELP_PI_MODEL_PATH`, `KELP_PI_MODEL_SHA256`, `KELP_PI_DATA_DIR` |
| `Dockerfile.api`, `Dockerfile.kelp`, `Dockerfile.kelpclaw` | Move to `legacy/docker/`                                                                                                                                |
| `docker-compose.yml`                                       | Move to `legacy/docker/`                                                                                                                                |

### 6.2 Verification step (post-excision)

- `grep -r "anthropic" app/ policies/ scripts/ docs/` → empty.
- `grep -r "openai" app/ policies/ scripts/ docs/` → empty (except as historical reference in `CHANGES.md`).
- No network dial in `app/` outside scanner sandbox + model download (download is install-time only, not runtime).

---

## 7. Policy / approval continuity

### 7.1 Port `appsec-agent-baseline`

Source: `packages/policy/src/packs.ts` (the `appsec-agent-baseline` pack).
Target: `policies/appsec-agent-baseline.toml`.

Constraints:

- Preserve every rule one-for-one.
- Provenance comment header: source file + git SHA at time of port.
- Same action verbs: `allow` / `deny` / `require-approval` / `log-only`.
- Same rule categories: destructive shell, secret exfil, unclassified tools, file writes, networked shell, agent delegation, irreversible actions.

### 7.2 Other packs to port (or drop)

| Pack                                                              | Disposition                                |
| ----------------------------------------------------------------- | ------------------------------------------ |
| `appsec-agent-baseline`                                           | **Port** — primary harness policy          |
| `baseline`                                                        | Port — general defaults                    |
| `no-destructive-shell`                                            | Port — narrow guard                        |
| `sg-agentic-ai-baseline`                                          | Port — region-specific, low cost           |
| `sg-pdpa-strict`, `sg-financial-ai`, `finance-sg`, `pii-strict`   | Port — declarative, no runtime cost        |
| `asean-genai-baseline`                                            | Port                                       |
| `github-pr-safe`                                                  | Drop — not applicable to Pi-resident agent |
| `web-search-safe`, `sg-web-research`, `browser-automation-strict` | Drop — no Internet by policy               |

### 7.3 Zig evaluator

- Regex (via `std.regex` or vendored `re2` if perf demands) + boolean expressions.
- Match surface: tool name + serialized tag attributes.
- Decision log: append to audit chain immediately on every evaluation, before approval prompt.

### 7.4 Approval reuse

Chat-REPL action approvals **reuse** the existing approval-token flow:

- Token TTL: 300s default (unchanged).
- Token storage: `/var/lib/kelp-pi/approvals/` (unchanged).
- Subcommand: `kelp-pi approve --data-dir <dir> <token>` (renamed from `kelp-pi-agent`).

---

## 8. Scanner & evidence parity

### 8.1 Scanner set (unchanged)

- **Nuclei**: active, vendored aarch64 binary in `vendor/nuclei/`, version pinned, templates pinned (commit `cce82b61d26bed35074cd57bc9d0aebd703a81d3` per current code). Sandboxed via systemd-run + unprivileged user + nftables mark.
- **Nmap, ZAP**: active, sandboxed.
- **SARIF, Nuclei JSONL, Nmap XML, ZAP JSON, Burp XML, Nessus XML**: passive imports.

### 8.2 Importer port table

| Source (`packages/evidence/src/`)     | Target (`app/evidence/`)                     | Notes                |
| ------------------------------------- | -------------------------------------------- | -------------------- |
| `sarif.ts`                            | `sarif.zig`                                  | Generic SARIF v2.1.0 |
| `nuclei.ts`                           | `nuclei.zig`                                 | JSONL                |
| `nmap.ts`                             | `nmap.zig`                                   | XML                  |
| `zap.ts`                              | `zap.zig`                                    | JSON                 |
| `burp.ts`                             | `burp.zig`                                   | XML                  |
| `nessus.ts`                           | `nessus.zig`                                 | XML                  |
| `index.ts` (workspace + signing + QA) | `evidence/workspace.zig` + `bundle/sign.zig` | Split by concern     |

### 8.3 Scope/gate behavior (unchanged)

- `kelp-pi scope set --host <target> --port <port> --until <rfc3339>` declares scope.
- Rate limits, target counts, duration caps preserved.
- nftables mark-based isolation preserved.
- No egress to anywhere outside scope during scan (unchanged).

---

## 9. Install / release

### 9.1 `scripts/install-kelp-pi.sh` rewrite

- Pi 5 aarch64 check (preserve `--allow-non-pi5` escape hatch).
- Download `kelp-pi-aarch64` static binary + verify SHA256.
- Download primary GGUF from `models/manifest.toml` + verify SHA256.
- Download Nuclei aarch64 + templates tarball + verify SHA256 (already in current script — preserve).
- Install systemd unit (preserve hardened directives: `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`, `NoNewPrivileges=true`, dedicated `kelp-pi` user). Target systemd-analyze security score < 3.0.
- Drop all references to Node, pnpm, TS bootstrap.

### 9.2 Release workflow

- GitHub Actions runner: x86-64 Linux.
- Build: `zig build -Doptimize=ReleaseSmall -Dtarget=aarch64-linux-gnu`.
- Artifacts: `kelp-pi-aarch64`, `kelp-pi-aarch64.sha256`, `kelp-pi-aarch64.sig` (signed by release key, separate from on-Pi identity key).
- Drop: Cargo build matrix, `cross`, `cargo-zigbuild`.

### 9.3 First-boot UX: `kelp-pi doctor`

Outputs in order:

1. Hardware probe: model string from `/proc/device-tree/model`, RAM total, NEON/dotprod/fp16 detection.
2. Memory budget: free RAM, estimated KV-cache ceiling for primary model.
3. Model registry status: which GGUFs present, hash verification.
4. Scanner readiness: Nuclei binary version, templates commit.
5. Policy status: active pack + version + hash.
6. systemd-analyze security score.
7. Signing key status: present, never-exported invariant intact.
8. Quota status: storage usage vs. limit.

Exit non-zero on any failure. `--strict` flag fails on warnings.

---

## 10. Verification / acceptance (physical Pi 5 4GB, today)

Smoke sequence to run during manual testing:

1. **Build & flash**: cross-compile on dev host, copy binary to Pi via SSH or USB, place GGUF in `/var/lib/kelp-pi/models/`.
2. **`kelp-pi doctor`**: clean exit (zero).
3. **Model load**: `kelp-pi model warm --id qwen3-0.6b-q4_k_m`. Record:
   - Time-to-first-token (TTFT)
   - Tokens/sec at 256-token generation
   - Peak RSS during generation
4. **Chat session golden path**: `kelp-pi chat`, import a fixture SARIF (use `fixtures/` from existing tree), ask "what is the highest-severity finding and why". Expect coherent answer with citation back to SARIF result index.
5. **`require-approval` flow**: ask the agent to scan an in-scope target with Nuclei. Confirm:
   - Policy evaluates to `require-approval`.
   - Approval prompt presented.
   - Approval token written to `/var/lib/kelp-pi/approvals/` with TTL.
   - Scanner runs only after `kelp-pi approve <token>`.
6. **`deny` flow**: ask the agent to scan an out-of-scope target. Confirm:
   - Policy evaluates to `deny`.
   - Audit chain entry written.
   - No subprocess spawned.
7. **Bundle generation + verify**: `kelp-pi bundle --run-id <id>`, copy bundle off Pi, run `kelp-pi verify-bundle <bundle>` on a clean host. Expect signature valid, transcript integrity verified, all referenced evidence present.

Measurement record (write into `docs/pi-launch.md` after the session):

- TTFT (ms)
- Throughput (tok/s)
- Peak RSS (MB)
- Cold-boot to chat-ready (s)
- Bundle verify time (ms)

---

## 11. Open questions / decisions deferred

1. **C++ fallback boundary**: confirmed needed only for vendored `llama.cpp`. Any other library demanding C/C++ surface? Candidate watchlist: SQLite (C, easy via Zig), nftables interaction (syscalls only, no lib).
2. **MCP adapter (`packages/adapters/src/mcp-adapter.ts`)**: keep for future Pi-side tool extensibility, or drop? Recommendation: drop in v1, add back behind a feature flag if a use case emerges.
3. **`web-intel`**: confirm full drop. Airgap operator + no Internet by policy ⇒ no surface. Anything salvageable (e.g., offline indexing of pre-downloaded HTML) goes to `app/retrieval/` if reused, otherwise legacy.
4. **Naming**: binary becomes `kelp-pi`. Project remains "KelpClaw". `kelp-claw` command alias is **dropped**, not aliased — single name reduces operator confusion.
5. **Session sharing**: do two operators on the same Pi (sequential SSH) share a chat session, or each gets their own? Recommendation: separate sessions per OS user, no cross-user read.
6. **`scripts/kelp-pi-ask`** (existing helper, 3.8k): retire or port? Currently appears to be a thin wrapper around `kelp-pi-agent ask`. Probably superseded by `kelp-pi chat` and the `ask` subcommand inside the new binary. Confirm during port.
7. **`packages/pi-agent/systemd/`** units: port directives verbatim to new install script. Confirm no Rust-specific assumptions in unit files (e.g., `ExecStart` path).
8. **Replay/equivalence smoke tests** (`scripts/pi-bundle-replay-smoke.mjs`, `scripts/pi-bundle-equivalence-smoke.mjs`): port to Zig integration tests or keep as `.mjs` until TS retirement? Recommendation: keep until cutover, then port to `app/bundle/replay_test.zig`.

---

## 12. Explicitly out of scope (today)

- Writing any Zig code.
- Editing `README.md`, `CHANGES.md`, any `docs/*.md`, or `docs/architecture.mmd`.
- Moving or deleting any package.
- Removing any cloud SDK from `package.json`.
- Building, flashing, or running the Pi.
- Downloading any GGUF.

All of the above is to be executed in subsequent sessions, against this todo as the source of truth.

---

## 13. Sources informing this plan

- [Best Open Source LLMs for Raspberry Pi 2026 — SiliconFlow](https://www.siliconflow.com/articles/en/best-open-source-LLMs-for-Raspberry-Pi)
- [On-Device Tool Calling 2026: Qwen3 vs Gemma 4 vs Phi-4-Mini — Ertas AI](https://www.ertas.ai/blog/on-device-tool-calling-2026-qwen3-gemma4-phi4)
- [LLMs on Raspberry Pi 5: Real Benchmarks — Local AI Master](https://localaimaster.com/blog/llm-raspberry-pi-5)
- [Aider edit formats](https://aider.chat/docs/more/edit-formats.html)
- [Aider 2026 setup & architect mode — DeployHQ](https://www.deployhq.com/guides/aider)
- [Cline GitHub](https://github.com/cline/cline)
- [Zig cross-compilation guide](https://zig.guide/build-system/cross-compilation/)
- [llama.cpp on ARM RK3588 / Pi — TuringPi 2026](https://turingpi.com/run-llm-locally-arm-rk3588-ollama-llama-cpp/)
