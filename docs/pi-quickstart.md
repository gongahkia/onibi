# Kelp Pi Quickstart

This is the operator path for a headless Raspberry Pi 5 running Raspberry Pi OS Lite
64-bit. Raspberry Pi 5 is a product requirement for the reference Kelp Pi path.

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
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh | sh -s -- --preflight-only
$ curl -fsSL https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh | sudo sh
$ kelp-pi preflight
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

## Laptop Control

Set SSH-on-LAN defaults from the laptop:

```console
$ pnpm --filter @kelpclaw/cli build
$ kelp-claw pi lab init \
  --host <pi-host> \
  --ssh-user <imager-ssh-user> \
  --control-url https://<control-plane-host>:443/health \
  --target-ip <fixture-ip> \
  --target-url http://fixture.local \
  --nuclei-approval-token <approval-token> \
  --client-a <ap-client-a-ip> \
  --client-b <ap-client-b-ip> \
  --forbidden-ip <non-portal-probe-ip>
```

Run Pi readiness checks and confirm the SSH agent path:

```console
$ kelp-claw pi doctor
$ kelp-claw pi doctor --strict --check-release-online
$ kelp-claw pi bootstrap --dry-run
$ kelp-claw pi connect
```

Validate the first-class hardened appliance profile:

```console
$ kelp-claw pi validate
```

The managed-AP acceptance surface is WPA3 AP mode, per-client isolation, local DNS
sinkhole behavior, outbound nftables allowlist, scoped scanner proof, and signed
bundle verification. The laptop writes the command result to
`.kelpclaw/pi/<host>/acceptance.json`; Pi-side validator logs stay under the remote
`--remote-output-dir` path, defaulting to `/var/lib/kelp-pi/bundles/field-acceptance-*`.
Each successful Pi-side field acceptance writes `acceptance-manifest.json`,
`acceptance-manifest.sig`, and `acceptance-manifest.pub.json`; verify them with:

```console
$ latest_acceptance="$(find /var/lib/kelp-pi/bundles -maxdepth 1 -type d -name 'field-acceptance-*' | sort | tail -n 1)"
$ kelp-pi-agent acceptance verify --artifact-dir "$latest_acceptance"
```

The lab profile is stored at `.kelpclaw/pi/lab.json`, which is gitignored.
If AP/firewall setup strands the unit, use the latest automatic snapshot:

```console
$ kelp-claw pi recover network --force --dry-run
$ kelp-claw pi recover network --force
```

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
