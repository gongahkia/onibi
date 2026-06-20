# Kelp Pi Quickstart

This is the operator path for a headless Raspberry Pi 5 running Raspberry Pi OS Lite
64-bit.

## Hardware

Minimum supported profile:

- Raspberry Pi 5
- 4GB RAM
- 56GB+ usable storage
- active cooling
- reliable USB-C power

4GB is for Nuclei/Nmap, local evidence retrieval, audit bundles, and small Ollama
models such as `qwen2.5:0.5b`. Use 8GB for 3B-class local models. Use 16GB for
ZAP-heavy or larger local synthesis profiles.

## Install

Default install downloads the latest `kelp-pi-agent-aarch64` GitHub release asset,
verifies `kelp-pi-agent-aarch64.sha256`, installs systemd files, installs Nuclei,
creates `/var/lib/kelp-pi`, starts the service, and runs doctor.

```console
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh | sudo sh
$ kelp-pi
```

If a release asset is not published yet, use the slower source-build path:

```console
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh | sudo sh -s -- --build-from-source
```

Inspectable install:

```console
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh -o /tmp/install-kelp-pi.sh
$ sudo sh /tmp/install-kelp-pi.sh
```

## First Run

```console
$ kelp-pi
$ kelp-pi version
$ kelp-pi doctor
$ kelp-pi logs
```

`kelp-pi` shows agent status, service status, RAM tier, storage, Nuclei status,
Ollama status, model catalog, and next commands.

## Local Model

List RAM-gated models:

```console
$ kelp-pi models
```

Install Ollama and pull the default 4GB-safe model:

```console
$ kelp-pi models install qwen2.5:0.5b
```

Show local models:

```console
$ kelp-pi models local
```

Remove a model:

```console
$ kelp-pi models remove qwen2.5:0.5b
```

## Update

Update the agent from the latest release asset. The command verifies SHA-256, keeps a
backup binary, restarts the service, runs doctor, and rolls back on failed health.

```console
$ kelp-pi update
```

Pin a release tag:

```console
$ kelp-pi update --release-tag v0.1.0
```

## Network Hardening

Network/AP hardening is not applied during install because it can disconnect headless
SSH. Apply only when you have local access or a recovery route.

```console
$ sudo kelp-pi network-render '<long-wpa3-passphrase>' --allow-outbound <control-plane-host>:443
$ sudo kelp-pi network-apply
$ sudo reboot
$ sudo kelp-pi validate-node
```

## Recovery

Wipe engagement data only:

```console
$ sudo kelp-pi wipe-data --force
```

Reset data layout and regenerate the Pi identity key:

```console
$ sudo kelp-pi reset --force
```

Uninstall Kelp Pi but keep data:

```console
$ sudo kelp-pi uninstall --force --keep-data
```

Uninstall Kelp Pi and remove data:

```console
$ sudo kelp-pi uninstall --force
```

## Real Pi Acceptance

Run this after a fresh OS install:

```console
$ kelp-pi
$ kelp-pi doctor
$ kelp-pi models install qwen2.5:0.5b
$ sudo kelp-pi validate-node
```

Full field acceptance still requires a target fixture, AP clients, DNS probes, and
allow-outbound reload evidence; see [`pi.md`](./pi.md).

## Troubleshooting

- Release missing: rerun install with `--build-from-source`.
- APT lock: installer waits up to 120 seconds, then exits with the current phase.
- Low disk: installer exits before clone/build/download.
- Service start fail: installer prints recent `journalctl -u kelp-pi-agent.service`.
- Model refused: run `kelp-pi models` and choose a model marked available for the
  detected RAM tier.
