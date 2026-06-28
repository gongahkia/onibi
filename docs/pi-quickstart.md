# Kelp Pi Quickstart

This is the target operator path for Raspberry Pi 5 running Raspberry Pi OS Lite 64-bit. Current commands can be tested on a dev host; real Pi acceptance is still open.

## Hardware

Minimum target:

- Raspberry Pi 5.
- 4GB RAM.
- 56GB+ usable storage.
- Active cooling.
- Reliable USB-C power.

8GB+ remains the practical profile for heavier scanner runs. 4GB is the floor for proving the 0.6B local model path. [Inference]

## Dev Host Smoke

```console
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm zig:test
$ pnpm zig:build
$ ./zig-out/bin/kelp-pi doctor --data-dir .kelp-pi
```

## First Run

```console
$ ./zig-out/bin/kelp-pi keygen --data-dir .kelp-pi --label dev-pi
$ ./zig-out/bin/kelp-pi model warm --id qwen3-0.6b-q4_k_m
$ ./zig-out/bin/kelp-pi chat --data-dir .kelp-pi
```

`model warm` currently verifies manifest presence only. It does not load GGUF until `llama.cpp` is linked.

## Scope And Approval

```console
$ ./zig-out/bin/kelp-pi scope set \
  --data-dir .kelp-pi \
  --host http://fixture.local \
  --until 2026-12-31T00:00:00Z

$ ./zig-out/bin/kelp-pi approval-request \
  --data-dir .kelp-pi \
  --scope-id default \
  --command 'nuclei http://fixture.local'

$ ./zig-out/bin/kelp-pi scan nuclei \
  --data-dir .kelp-pi \
  --target http://fixture.local \
  --approval-token <token> \
  --dry-run
```

Expected behavior:

- Outside-scope targets fail.
- Active scanner commands require an approval token.
- Denied exploit/destructive commands are refused before execution.

## Evidence

```console
$ printf '{"finding":"default admin marker"}\n' > .kelp-pi/finding.json
$ ./zig-out/bin/kelp-pi index ingest \
  --data-dir .kelp-pi \
  --input .kelp-pi/finding.json \
  --path evidence/finding.json
$ ./zig-out/bin/kelp-pi ask default --data-dir .kelp-pi
```

Current index storage is JSONL. SQLite FTS5 is the target implementation.

## Bundle

```console
$ ./zig-out/bin/kelp-pi bundle assemble \
  --run-id local \
  --workspace .kelp-pi \
  --output .kelp-pi/bundles/local
$ ./zig-out/bin/kelp-pi verify-bundle .kelp-pi/bundles/local
```

Current bundle verification checks required files only. Signature verification is still open.

## Real Pi Target

Release install target:

```console
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh | sudo sh
$ kelp-pi doctor
$ kelp-pi model warm --id qwen3-0.6b-q4_k_m
$ kelp-pi chat
```

Installer rewrite is still open. Current installer still reflects the legacy Rust/Ollama path.

## Recovery

Until the installer is rewritten, avoid applying network/AP hardening from old docs. Hardened AP mode, DNS sinkholing, nftables allowlists, and read-only root are target controls, not current Zig scaffold behavior.
