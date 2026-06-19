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
- [ ] Implement `selfcheck.run` that reports: AP state, SSID, IP, isolation rule presence, allowlist contents, listening ports, free disk, RAM, CPU temperature, microSD wear estimate, audit-log verify result → success: a `selfcheck.report` envelope contains all fields and is signed.
- [ ] Make `selfcheck` refuse any target outside `127.0.0.0/8`, the Pi's own AP CIDR, and the configured allowlist → success: attempting to point selfcheck at `8.8.8.8` is logged and refused with an explicit error.
- [ ] Disable Bluetooth, audio, HDMI, and any unused peripheral via boot config → success: `dmesg` post-boot does not show the disabled subsystems initialized.
- [ ] Pin Pi OS kernel and packages to a known good version; document upgrade procedure → success: `kelp/docs/pi.md` includes the pin and the manual upgrade flow that re-runs selfcheck before promotion.
- [ ] Optional: configure read-only root with a writable overlay for `/var/lib/kelp-pi` → success: an integration test pulls the SD card during write activity, reboots, and the agent comes up cleanly.

## P3 — Retrieval foundation

- [ ] Add `--json` flag to the Python client → success: machine-readable JSON output suitable for scripting.
- [ ] Implement re-ingest detection: if any source file's mtime or hash changed, the chunk rows for that file are replaced atomically → success: an integration test edits a file, re-ingests, and the prior chunk IDs for the unchanged sections remain stable.
- [ ] Add a `stale-index` flag in selfcheck when source files exist that have never been ingested → success: dropping a new file into the corpus dir surfaces a warning in the next selfcheck.

## P4 — Scanner integration

- [ ] Embed Nuclei as a pinned ARM64 binary in the Pi image → success: `kelp-pi-agent scan nuclei --target ...` runs without external `nuclei` install.
- [ ] Pin a Nuclei templates revision (git SHA) and document update flow → success: the agent records the templates SHA in every scan envelope; updates are explicit operator action.
- [ ] Embed or depend on system Nmap with a pinned version → success: agent records nmap version in every scan envelope; refuses to run if version mismatch is critical.
- [ ] Implement a `scope.set` handler: operator declares an allowed-target list (CIDR/host/port) and a time window → success: scope is signed, persisted, and queried by every scan command before execution.
- [ ] Implement out-of-scope hard block: any scanner target outside the current scope is refused with a logged reason → success: attempting to scan an out-of-scope target produces an audit-log `scope.violation` entry and a non-zero exit.
- [ ] Implement rate limits per target: max requests/sec, max concurrent targets, max scan duration → success: limits enforced; an integration test confirms they are respected.
- [ ] Implement scanner output normalization into the kelp evidence schema → success: a Nuclei JSONL output is converted to evidence rows that pass `kelp/packages/evidence` validation.
- [ ] Implement ZAP integration as an opt-in feature flag, only enabled on 16GB Pi or non-Pi hosts → success: on 8GB Pi the agent refuses to enable ZAP with an explicit message; on 16GB Pi it works.
- [ ] Implement scanner job lifecycle: `scan.request → scan.event* → scan.complete` envelopes → success: a control-plane consumer can rebuild a timeline of a scan from envelopes alone.
- [ ] Implement pre-flight policy check before any scan → success: a scan refused by policy never invokes the scanner binary; the refusal is logged.
- [ ] Implement scanner sandboxing: each scanner runs as an unprivileged user under systemd-run with no network access except to in-scope targets via nftables marks → success: a scanner invocation cannot reach the control plane or the public internet.

## P5 — Policy gates on Pi

- [ ] Port `appsec-agent-baseline` decision vocabulary into the Rust agent → success: same decision names (`allow`, `deny`, `require-approval`) and same rule IDs are recognized.
- [ ] Implement signed policy-pack delivery via `policy.push` → success: the agent loads only policy packs signed by a key on its trust list.
- [ ] Implement local policy evaluation for scanner invocations, file operations, and outbound network requests → success: every gated action produces a `policy-decision` audit entry.
- [ ] Implement operator-approval flow for `require-approval` decisions: the agent prints a one-time approval token to a local TTY; operator runs `kelp-claw pi approve <token>` over SSH to confirm → success: an integration test demonstrates a scan that is blocked until approved and then proceeds.
- [ ] Make approvals time-limited (default 15 min) and scope-bound → success: an expired approval token is rejected; an approval issued for one scope cannot authorize a different scope's scan.
- [ ] Add a policy `dry-run` mode that logs decisions without acting → success: operators can rehearse an engagement and review the policy decisions before live runs.

## P6 — Audit bundle parity

- [ ] Define on-Pi audit bundle directory layout matching `kelp/packages/evidence` expectations → success: a bundle exported by the Pi has the same top-level files (`audit-bundle/index.html`, `manifest.json`, `signature`, `attestation`, `findings.sarif`, etc.) as a laptop-produced bundle.
- [ ] Implement bundle assembly in Rust using the same manifest schema as the TS code → success: a bundle exported by the Pi passes `kelp-claw verify-audit-bundle` with no Pi-specific flags or shims.
- [ ] Add a cross-implementation test in CI that produces a bundle from a fixture run on both the TS path and the Rust path and asserts equivalence (modulo timestamps and signatures) → success: this test exists and is green.
- [ ] Implement static `index.html` generation for the Pi-produced bundle → success: opening the file in a browser shows findings, evidence sources, policy decisions, and a chain-of-custody section.
- [ ] Include the hash-chained audit log in the bundle → success: bundle contains the relevant audit-log slice and the chain verifies against the Pi's public key.

## P7 — Control plane sync

- [ ] Implement `bundle.export` from Pi: marshals a completed audit bundle into an envelope and ships it to the control plane → success: the bundle arrives on the control plane intact and verifies.
- [ ] Implement `bundle.fetch` reverse: control plane can pull a bundle by run-id → success: a CLI command on the laptop retrieves and verifies a Pi-produced bundle.
- [ ] Implement `policy.pull` on Pi: agent requests current signed policy packs at startup and on a configurable interval → success: rotating a pack on the control plane propagates to Pi on next pull and is logged.
- [ ] Implement `scope.set` flow that originates on the laptop CLI and pushes to Pi → success: `kelp-claw pi scope set --cidr ... --until ...` results in the Pi having the scope persisted and signed.
- [ ] Implement offline tolerance: the Pi continues operating with the last good policy and scope if the control plane is unreachable → success: an integration test severs the network mid-engagement and the Pi continues to honor existing scope and policy.
- [ ] Implement replay-after-reconnect: queued envelopes from Pi to control plane are sent in order when connectivity returns → success: integration test confirms ordered delivery after a disconnection.

## P8 — Optional LLM synthesis

- [ ] Add a feature-flagged Ollama integration on the Pi (16GB only) → success: a 3B-class model can be loaded; loading on 8GB Pi is refused with a clear error.
- [ ] Implement retrieval-then-generate flow with citation enforcement: every generated sentence must reference at least one citation in the prompt context or the entire generation is rejected → success: a unit test confirms citation-free generations are rejected.
- [ ] Implement strict no-answer escalation: if retrieval returns no-answer, generation is skipped entirely → success: an unrelated query never produces generated text.
- [ ] Keep synthesis off by default; require an explicit `--synthesize` flag on the client and a policy decision on the Pi → success: default `/ask` response contains citations only, no generated prose.
- [ ] Add a synthesis-eval harness: a small Q/A set with expected citations and gold answers tracks regression in no-answer behavior and citation faithfulness → success: harness runs in CI as a nightly job.

## P9 — Eval and determinism

- [ ] Build a gold Q/A set against a small fixture corpus → success: 30+ Q/A pairs each with expected chunk IDs and expected `no_answer` cases.
- [ ] Implement a `kelp-pi-agent eval gold` subcommand that runs the gold set and reports pass/fail per case → success: command exits non-zero if any expected source chunk is missing from retrieved citations.
- [ ] Implement reproducible-chunk-ID regression: same corpus must produce same chunk IDs across Pi, laptop, and CI runners → success: a CI job rebuilds the index on three environments and diffs the chunk-ID set, failing on any drift.
- [ ] Implement bundle-replay test: a Pi-produced bundle can be re-verified end-to-end on a laptop with no Pi present → success: CI runs this as a regression.
- [ ] Implement scanner-output stability test: same target, same templates, same nmap version produces structurally identical evidence rows modulo timing → success: drift detection alerts on any new field or removed field.
- [ ] Add an audit-log forensics test: deliberately mutate a log entry and confirm verification fails at the expected position → success: test is green and fails fast if the verify-audit-log routine regresses.

## P10 — Field ops

- [ ] Read Pi battery state (via USB-C PD or HAT) if present and surface in selfcheck → success: selfcheck report includes battery percentage when available, null otherwise.
- [ ] Read CPU temperature and throttle state; refuse to start new scans when thermal-throttled → success: starting a scan at >80C logs a refusal and waits or aborts per config.
- [ ] Implement storage quota enforcement: refuse to ingest, scan, or accept uploads when free disk falls below a configurable floor → success: integration test fills disk and confirms graceful refusal with operator-readable error.
- [ ] Implement audit log rotation with signed segment manifests → success: rotation produces a new segment file; the chain across segments verifies.
- [ ] Implement microSD wear monitoring via SMART or kernel counters where possible → success: selfcheck surfaces a warning when wear exceeds a threshold.
- [ ] Document recovery mode: how to boot a Pi with corrupted data dir, recover keys, and replay the last good bundle from the control plane → success: a written recovery runbook exists in `kelp/docs/pi-recovery.md`.
- [ ] Implement a `kelp-claw pi flash` command that writes a configured image to an SD card or NVMe → success: command writes a verified image and pre-seeds the operator's public key for SSH.
- [ ] Implement signed firmware-update path: agent verifies update bundle signature before applying → success: an unsigned or wrong-key update is refused and logged.
- [ ] Add a `kelp-claw pi wipe` command for secure end-of-engagement decommission → success: command zeros the data dir and the agent refuses to start without re-bootstrap.

## P11 — Docs, demo, launch

- [ ] Write `kelp/docs/pi.md` operator quickstart: flash, scope, scan, bundle, verify → success: a new operator following the doc produces a signed bundle from a fixture target in under 30 minutes.
- [ ] Add a sample vulnerable Docker target and a sample engagement walkthrough that exercises Pi end-to-end → success: walkthrough produces a complete bundle that verifies and demonstrates each P1-P10 capability.
- [ ] Record a short demo: SSH into Pi, declare scope, run a scan, retrieve a citation, export bundle → success: demo asset (cast or gif) committed under `kelp/docs/assets/`.
- [ ] Update kelp top-level README with a Pi section linking to `docs/pi.md` → success: README explains the Pi target in one paragraph and one diagram.
- [ ] Draft an honest comparison section vs xPrep, IIAB, Hak5, Pwnagotchi, pi-local-rag → success: `kelp/docs/pi-prior-art.md` exists and is fair.
- [ ] Add a CHANGELOG entry for the Pi target introduction → success: `kelp/CHANGES.md` updated.
- [ ] Open a tracking issue on the kelp repo to coordinate P1-P11 → success: issue exists with checklist linking to this file.
- [ ] Prepare a Show HN posture: one-line value prop, 30-second README hook, demo gif, honest non-goals, no overclaiming → success: a draft `kelp/docs/pi-launch.md` exists and a teammate (or future-you) signs off.

## Acceptance gate (block release until all checked)

- [ ] All P0 decisions recorded and linked from `kelp/docs/pi.md`.
- [ ] Cold-start engagement demo passes in under 30 minutes from a freshly flashed Pi.
- [ ] Pi-produced bundle verifies under `kelp-claw verify-audit-bundle` unchanged.
- [ ] `/ask` returns cited chunks offline; unrelated queries return `no_answer`.
- [ ] Out-of-scope scan attempts are blocked and audited.
- [ ] Selfcheck refuses non-local targets and flags unexpected listening ports.
- [ ] Reproducible-chunk-ID test passes across Pi, laptop, and CI.
- [ ] Power-loss-mid-scan test passes: audit log verifies on next boot; run is marked resumable.
- [ ] Threat model in `docs/pi-threat-model.md` matches what the code actually enforces.
- [ ] Honest non-goals are documented and visible in the README.

## Rename note

`bloob/` was retired as of 2026-06-19. The product is **KelpClaw**, the Pi-specific
surface is **Kelp Pi**, the agent binary is **kelp-pi-agent**, the CLI subcommand
family is `kelp-claw pi ...`. Do not reintroduce "bloob" as a name anywhere.
