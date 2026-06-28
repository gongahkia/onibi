# Kelp Pi TODO

Source of truth for the local-only Pi pivot. Keep tasks one line where possible.

## P0 - Pivot Contract

- [x] Rewrite README around Pi-resident `kelp-pi`.
- [x] Record local-only runtime: no cloud APIs, no laptop control plane, no provider table, no Ollama daemon.
- [x] Add Zig build entrypoint: `build.zig`.
- [x] Add Zig command scaffold: `app/main.zig`.
- [x] Add TOML policy pack: `policies/appsec-agent-baseline.toml`.
- [x] Add model manifest: `models/manifest.toml`.
- [x] Add package scripts for Zig build/test/format.
- [x] Fill final GGUF SHA-256 after downloading the selected model.
- [ ] Add release gate that fails when model SHA-256 is blank.

## P1 - Zig Parity

- [x] Implement `kelp-pi doctor`.
- [x] Implement `kelp-pi keygen` with stdlib Ed25519 generation.
- [x] Implement `kelp-pi policy check`.
- [x] Implement `kelp-pi scope set`.
- [x] Implement approval token creation and approved-token enforcement.
- [x] Implement dry-run scanner gate with policy + scope + approval checks.
- [x] Implement SQLite FTS5 `index ingest` and `ask`.
- [x] Implement signed `bundle assemble` and `verify-bundle`.
- [x] Implement `model fetch`, `model verify`, and `model warm` hash/GGUF checks.
- [x] Implement `chat` shell scaffold.
- [ ] Split `app/main.zig` into modules after behavior stabilizes.
- [ ] Port Rust `policy_sync`, `scan_scope`, `acceptance_manifest`, `storage_quota`, and `outbox_replay` tests.

## P2 - llama.cpp

- [x] Vendor or submodule `llama.cpp`.
- [x] Add optional `build.zig -Dllama=true` link path for libllama.
- [x] Add RAM gate before model load.
- [ ] Load Qwen3 0.6B Q4_K_M GGUF on Pi 5 4GB.
- [ ] Record token latency, peak RSS, thermals, and failure mode.

## P3 - Retrieval

- [x] Replace JSONL stub with SQLite FTS5.
- [x] Store chunk IDs from path + content hash.
- [ ] Require citations for every generated finding.
- [ ] Add malicious corpus tests for prompt injection and binary ingest refusal.

## P4 - Scanner Orchestration

- [ ] Pin Nuclei/Nmap/ZAP install or bundle strategy for aarch64.
- [x] Add unprivileged `systemd-run` sandbox command path.
- [x] Reload nftables scanner target set before sandboxed execution.
- [ ] Persist raw scanner output and normalized findings.

## P5 - Bundles And Audit

- [ ] Implement append-only transcript JSONL.
- [ ] Implement hash-chained audit log.
- [x] Sign bundle manifest with Pi Ed25519 key.
- [x] Verify bundle hashes and signatures on host.
- [ ] Preserve legacy replay/equivalence smoke coverage until Zig parity.

## P6 - Installer And Ops

- [ ] Rewrite `scripts/install-kelp-pi.sh` for `kelp-pi-aarch64`.
- [ ] Port hardened systemd directives from `packages/pi-agent/systemd/`.
- [ ] Add systemd-analyze security target below 3.0.
- [ ] Add network/AP hardening only after a recovery path exists.
- [x] Add SSH acceptance harness using `.kelp-pi/acceptance.env`.
- [ ] Run SSH acceptance on real Pi and capture evidence.

## Cutover Gate

- [ ] One fresh Raspberry Pi 5 run produces a signed bundle from a fixture target in under 30 minutes.
- [ ] `pnpm verify`, `zig build test`, and `zig build` pass.
- [ ] Legacy TS/Rust runtime paths are moved only after equivalent Zig behavior exists.
