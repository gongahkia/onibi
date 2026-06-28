# Kelp Pi Quickstart

This is the target operator path for Raspberry Pi 5 running Raspberry Pi OS Lite 64-bit. Current commands can be tested on a dev host; real Pi acceptance needs the Linux/aarch64 package, a reachable Pi, and `.kelp-pi/acceptance.env`.

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

## Linked Llama Smoke

```console
$ pnpm llama:build
$ pnpm zig:build:llama
$ pnpm llama:smoke
$ pnpm pi:package
```

`llama:smoke` builds local `libllama`, links `kelp-pi` with `-Dllama=true`, loads the pinned GGUF, decodes greedy tokens, and verifies JSON reports `loaded:true`.

## Linux/aarch64 Package

```console
$ pnpm llama:build:aarch64
$ pnpm zig:build:aarch64
$ pnpm pi:package:aarch64
$ pnpm pi:preflight:aarch64
```

This builds inside Docker `linux/arm64`, targets Raspberry Pi OS Bookworm 64-bit / glibc 2.36 by default, packages `bin/kelp-pi` plus `libllama.so*`/`libggml*.so*`, and verifies the packaged binary is ELF aarch64 before live Pi testing.

## First Run

```console
$ ./zig-out/bin/kelp-pi keygen --data-dir .kelp-pi --label dev-pi
$ ./zig-out/bin/kelp-pi model warm --id qwen3-0.6b-q4_k_m
$ ./zig-out/bin/kelp-pi chat --data-dir .kelp-pi
```

`model warm` verifies manifest SHA-256, GGUF magic, and RAM floor. With `zig build -Dllama=true -Dllama-prefix=.kelp-pi/llama/host`, it also loads `libllama` and decodes a small prompt.

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

$ ./zig-out/bin/kelp-pi approve --data-dir .kelp-pi <token>

$ ./zig-out/bin/kelp-pi scan nuclei \
  --data-dir .kelp-pi \
  --target http://fixture.local \
  --approval-token <token> \
  --dry-run
```

Expected behavior:

- Outside-scope targets fail.
- Active scanner commands require an approved, unexpired approval token.
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

Index storage is SQLite FTS5 at `.kelp-pi/index/chunks.sqlite3`.

## Bundle

```console
$ ./zig-out/bin/kelp-pi bundle assemble \
  --run-id local \
  --workspace .kelp-pi \
  --output .kelp-pi/bundles/local
$ ./zig-out/bin/kelp-pi verify-bundle .kelp-pi/bundles/local
```

Bundle verification checks manifest signature and file SHA-256 values.

## SSH Acceptance

Create `.kelp-pi/acceptance.env`:

```sh
KELP_PI_SSH_HOST=<pi-host>
KELP_PI_SSH_USER=<pi-user>
KELP_PI_PACKAGE_DIR=.kelp-pi/dist/kelp-pi-linux-aarch64
KELP_PI_VERIFY_BINARY=./zig-out/bin/kelp-pi
```

Then run:

```console
$ pnpm accept:pi
```

The script copies the binary, llama shared libs, and local GGUF. It requires remote `doctor` to report `llama-linked=pass`, remote `model warm` to report `loaded:true`, records host/binary/ldd/model/thermal evidence under `.kelp-pi/acceptance-evidence`, then runs doctor/scope/approval/scan/index/ask/bundle on the Pi, copies the bundle back, and verifies it locally.

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
