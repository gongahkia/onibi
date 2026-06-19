# Kelp Pi — TODO

One task per line. Each task ends with `→ success: <condition>` so completion is
unambiguous. Order roughly follows phases in [`pi.md`](./pi.md). Tasks within a
phase may parallelize; tasks across phases generally cannot.

If you are picking this up cold: read [`pi.md`](./pi.md) first for the design,
[`appsec-harness.md`](./appsec-harness.md) for the AppSec audit surface that Kelp
Pi extends, and [`deployment.md`](./deployment.md) for control-plane runtime
conventions. Then start at the first unchecked P0 task below.

## P0 — Scoping, absorption, and contracts

- [x] Migrate IDEA and TODO into `kelp/docs/pi.md` and `kelp/docs/pi-todo.md` → success: both files live under `kelp/docs/`; `bloob/` reduced to a stub pointer (full deletion deferred to the operator's git workflow).
- [x] Update kelp top-level README to mention the Pi target in one paragraph → success: README contains a "Kelp Pi (in design)" section linking to `docs/pi.md` and `docs/pi-todo.md`.

## P1 — Rust agent foundation

- [ ] Cross-compile to `aarch64-unknown-linux-gnu` via `cross` or `cargo-zigbuild` → success: a `kelp-pi-agent` binary runs on a real Pi 5 and prints version.
- [ ] Provide a systemd unit file `kelp-pi-agent.service` → success: `systemctl start kelp-pi-agent` brings up the daemon and `status` shows healthy.
- [ ] Configure systemd unit with `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`, `NoNewPrivileges=true`, dedicated user → success: `systemd-analyze security kelp-pi-agent.service` scores below 3.0 (lower is better).
- [ ] Implement graceful shutdown on SIGTERM that drains in-flight work or marks it resumable → success: integration test kills the daemon during a scan and the next start marks the run resumable.

## P2 — Pi networking and hardening

- [ ] Configure NetworkManager AP mode for wlan0 with WPA3-only and per-client isolation → success: two clients on the AP cannot ping each other; only the Pi's portal IP responds.
- [ ] Disable upstream DNS by default; install a local dnsmasq with sinkhole rules for captive-portal probes → success: clients connecting see the local portal IP for `captive.apple.com`, `connectivitycheck.gstatic.com`, `clients3.google.com`, etc., and zero upstream DNS queries leave the Pi.
- [ ] Configure nftables outbound allowlist: deny all egress except declared control-plane endpoints → success: `nft list ruleset` shows the allowlist; `curl https://example.com` from the Pi fails with no route.
- [ ] Add an `allow-outbound` config field that takes a list of `host:port` pairs and rewrites the nftables rules atomically → success: changing the config and reloading does not drop the existing control-plane session.
- [ ] Disable Bluetooth, audio, HDMI, and any unused peripheral via boot config → success: `dmesg` post-boot does not show the disabled subsystems initialized.
- [ ] Optional: configure read-only root with a writable overlay for `/var/lib/kelp-pi` → success: an integration test pulls the SD card during write activity, reboots, and the agent comes up cleanly.

## P3 — Retrieval foundation

## P4 — Scanner integration

- [ ] Embed Nuclei as a pinned ARM64 binary in the Pi image → success: `kelp-pi-agent scan nuclei --target ...` runs without external `nuclei` install.
- [ ] Implement scanner sandboxing: each scanner runs as an unprivileged user under systemd-run with no network access except to in-scope targets via nftables marks → success: a scanner invocation cannot reach the control plane or the public internet.

## P5 — Policy gates on Pi

## P6 — Audit bundle parity

- [ ] Add a cross-implementation test in CI that produces a bundle from a fixture run on both the TS path and the Rust path and asserts equivalence (modulo timestamps and signatures) → success: this test exists and is green.

## P7 — Control plane sync

- [ ] Implement `bundle.export` from Pi: marshals a completed audit bundle into an envelope and ships it to the control plane → success: the bundle arrives on the control plane intact and verifies.
- [ ] Implement `policy.pull` on Pi: agent requests current signed policy packs at startup and on a configurable interval → success: rotating a pack on the control plane propagates to Pi on next pull and is logged.
- [ ] Implement offline tolerance: the Pi continues operating with the last good policy and scope if the control plane is unreachable → success: an integration test severs the network mid-engagement and the Pi continues to honor existing scope and policy.
- [ ] Implement replay-after-reconnect: queued envelopes from Pi to control plane are sent in order when connectivity returns → success: integration test confirms ordered delivery after a disconnection.

## P8 — Optional LLM synthesis

- [ ] Add a feature-flagged Ollama integration on the Pi (16GB only) → success: a 3B-class model can be loaded; loading on 8GB Pi is refused with a clear error.

## P9 — Eval and determinism

## P10 — Field ops

- [ ] Implement storage quota enforcement: refuse to ingest, scan, or accept uploads when free disk falls below a configurable floor → success: integration test fills disk and confirms graceful refusal with operator-readable error.
- [ ] Implement a `kelp-claw pi flash` command that writes a configured image to an SD card or NVMe → success: command writes a verified image and pre-seeds the operator's public key for SSH.

## P11 — Docs, demo, launch

- [ ] Write `kelp/docs/pi.md` operator quickstart: flash, scope, scan, bundle, verify → success: a new operator following the doc produces a signed bundle from a fixture target in under 30 minutes.
- [ ] Add a sample vulnerable Docker target and a sample engagement walkthrough that exercises Pi end-to-end → success: walkthrough produces a complete bundle that verifies and demonstrates each P1-P10 capability.
- [ ] Record a short demo: SSH into Pi, declare scope, run a scan, retrieve a citation, export bundle → success: demo asset (cast or gif) committed under `kelp/docs/assets/`.

## Acceptance gate (block release until all checked)

- [ ] Cold-start engagement demo passes in under 30 minutes from a freshly flashed Pi.
- [ ] Power-loss-mid-scan test passes: audit log verifies on next boot; run is marked resumable.

## Rename note

`bloob/` was retired as of 2026-06-19. The product is **KelpClaw**, the Pi-specific
surface is **Kelp Pi**, the agent binary is **kelp-pi-agent**, the CLI subcommand
family is `kelp-claw pi ...`. Do not reintroduce "bloob" as a name anywhere.
