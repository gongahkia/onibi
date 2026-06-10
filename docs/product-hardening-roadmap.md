# Product Hardening Roadmap

KelpClaw is focused on reproducible AppSec agent triage: scoped target builds, passive scanner evidence, policy-gated agent review, SARIF, signed audit bundles, and reviewer-friendly evidence handoff.

## Implemented

- `kelp-claw appsec audit` builds a Dockerfile target, records build metadata, imports passive scanner output, runs a scoped triage assistant, and exports a signed audit bundle.
- `appsec-agent-baseline` blocks destructive shell, credential exfiltration, exploit execution, persistence, and lateral movement by default.
- Evidence workspaces import SARIF, Nmap, Nuclei, Burp, ZAP, and Nessus outputs.
- Static audit bundles include manifests, signatures, attestations, SARIF, logs, policy decisions, and review HTML.
- Repository inventory and skill audit remain available as supporting surfaces.

## Next Product Directions

1. AppSec harness ergonomics
   - Add a sample vulnerable Docker app and sample triage agent.
   - Add `mode: appsec` coverage to the GitHub Action.
   - Improve reviewer HTML for AppSec-specific findings.

2. Evidence quality
   - Add correlation between imported scanner findings and agent triage findings.
   - Add stable finding IDs across reruns.
   - Add evidence QA checks for missing source refs and scanner drift.

3. Safe validation
   - Add explicit lab-mode local PoC validation with allowlisted commands and target network isolation.
   - Keep exploit execution out of default mode.

4. Release hardening
   - Publish npm package and Homebrew tap.
   - Add release provenance and external signing key support.

5. Benchmarks
   - Add small AppSec fixture apps with expected scanner evidence and triage outputs.
   - Track parser drift, SARIF output stability, and audit bundle verification.
