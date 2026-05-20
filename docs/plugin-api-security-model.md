# Plugin API Boundaries And Security Model

Status: accepted design boundary for future third-party plugin work.

## Purpose

Piranesi may support third-party extensions after PFF, SDKs, and validation are
stable. Plugins must extend artifact import/export and report assembly without
bypassing local-first evidence ownership, provenance, redaction, or validation.

This document defines allowed plugin roles and the security constraints that
implementation issues must preserve.

## Allowed Plugin Roles

- Import adapters that read external tool exports and emit valid PFF.
- Export adapters that transform local report/PFF artifacts into downstream
  handoff formats.
- Report section renderers that add bounded text sections from existing workspace
  data.
- Validators that inspect PFF, report bundles, and workspace-derived artifacts.

Plugins may not become autonomous scanners, exploitation modules, C2 controllers,
credential harvesters, or report approval authorities.

## Trust Model

- Plugins are untrusted by default.
- Plugin output is untrusted until it passes PFF/report/workspace validation.
- A plugin must not receive raw secrets unless the operator explicitly selects
  the input file or artifact for that plugin.
- The local workspace remains the source of truth. Plugin output is imported as
  proposed data, not silently accepted report truth.
- Human approval remains required for delivery, report changes, and any external
  handoff that leaves the local machine.

## Execution Boundary

The preferred execution model is process isolation:

- Run plugins out of process.
- Pass inputs through explicit files or stdin/stdout contracts.
- Require deterministic JSON/PFF output.
- Capture plugin version, command, input digests, output digests, and exit status.
- Fail closed when validation fails.

In-process plugin loading is deferred until there is a stronger signing and
capability story. Marketplace or registry mechanics are out of scope until this
boundary is implemented and tested.

## Filesystem Access

- Plugins may read only operator-selected input files and explicitly provided
  workspace artifacts.
- Plugins may write only to a staging/output directory owned by the invocation.
- Plugins must not mutate `workspace.json`, `normalized/findings.json`, evidence
  indexes, signatures, audit logs, or report artifacts directly.
- Piranesi imports plugin output through normal validated ingest/import paths.

## Network Access

- Network access is denied by default.
- Network-enabled plugins require an explicit future capability flag and a separate
  threat model.
- Plugins must not open reverse shells, run payloads, contact targets, call C2
  infrastructure, or exfiltrate workspace data.

## Secrets Handling

- Piranesi does not pass GitHub, Slack, email, model-provider, or workspace secrets
  into plugins by default.
- Plugin logs, manifests, and error messages must not include tokens, webhook
  URLs, cookies, passwords, API keys, or private keys.
- Secret-like evidence should be redacted before plugin output becomes PFF.

## Required Validation

- Import plugins emit PFF and must pass `piranesi ci validate-pff`.
- Export/report plugins must pass `piranesi ci validate-report-bundle` where
  applicable.
- Plugin output must preserve source references, provenance, and known-gaps
  metadata.
- Validation failures are actionable errors, not warnings.

## Explicitly Forbidden

- Direct workspace mutation.
- Hidden network calls.
- Autonomous testing, scanning, exploitation, payload generation, or target
  interaction.
- Raw evidence upload to third-party services.
- Silent report text changes.
- Disabling redaction, provenance, chain-of-custody, or validation checks.
- Storing credentials in plugin manifests, fixtures, or generated artifacts.
