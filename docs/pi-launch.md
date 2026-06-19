# Kelp Pi Launch Draft

Status: draft posture, self-reviewed 2026-06-19.

## One-Line Value Prop

Kelp Pi is a portable, audit-first AppSec evidence appliance for scoped on-site
triage: local retrieval, policy gates, hash-chained logs, and signed reviewer
handoff.

## 30-Second README Hook

Kelp Pi turns a Raspberry Pi 5 into a field data plane for AppSec engagements. It
keeps target evidence local, refuses work outside declared scope, records policy
decisions, answers `/ask` with cited local chunks, and exports signed audit bundles
that reviewers can verify later. It is designed for small teams that need a portable
evidence appliance, not a covert implant or general internet scanner.

## Demo Cast Script

Target length: 20-30 seconds.

1. Start `examples/pi-vulnerable-target/record-pi-demo.sh` against the freshly
   flashed Pi and private fixture target.
2. The recorded walkthrough SSHes into the Pi, declares fixture scope, approves the
   scanner gate, runs pinned Nuclei through the sandbox, runs field acceptance,
   assembles/fetches the bundle, and verifies the reviewer bundle.
3. End only after `scripts/verify-pi-launch-evidence.sh` accepts the cast, fetched
   bundle, field-acceptance logs, and signed bundle verifier output.

Do not commit a demo cast from an unverified run. The default committed asset path is
`docs/assets/kelp-pi-fixture-demo.cast`.

## Honest Non-Goals

- Not a rogue access point, Wi-Fi attack toy, or covert implant.
- Not an exploit runner by default.
- Not an internet-wide scanner.
- Not a replacement for legal evidence retention or independent timestamping.
- Not a promise that Pi hardware alone makes evidence admissible.
- Not finished until the remaining hardware, scanner, bundle, and sync TODOs are
  complete.

## Claims To Avoid

- "Production ready."
- "Tamper-proof."
- "Legally admissible."
- "Runs any scanner safely."
- "Works on Pi 5" before a real Pi 5 run prints version and passes selfcheck.
- "Encrypted key custody" until the implemented key file format actually encrypts
  private key material.

## Current Safe Claims

- The repo has a Rust `kelp-pi-agent` with data-dir preflight, key generation, audit
  log verification, local policy decisions, approval tokens, deterministic chunking,
  SQLite FTS5 retrieval, `/ask`, gold eval, and chunk-ID CI regression.
- The launch remains a build-in-public technical preview while P1/P2/P4/P6/P7/P10
  hardware and bundle tasks are unfinished.

## Sign-Off

Future-you review on 2026-06-19: acceptable as a draft posture because it names
unfinished work, avoids production claims, and points the demo at implemented local
flows only.
