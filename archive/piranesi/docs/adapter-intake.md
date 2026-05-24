# Adapter Intake Gate

Date: 2026-05-20

Status: required before new adapter implementation.

Future import adapters must start from real authorized evidence, not from a
format idea or hand-authored fixture. This gate applies to long-tail offensive
tool outputs, host posture exports, and any third-party adapter that would add
normalized findings or preserved raw evidence.

## Required Intake Record

Open one GitHub issue per candidate adapter and include:

- tool name, version, and exact export command or UI export path;
- source format, file extensions, and whether the export is single-file or
  archive-based;
- target context and authorization note for the lab or engagement that produced
  the export;
- sanitization notes covering hostnames, usernames, secrets, tokens, payloads,
  client labels, and credentials;
- SHA-256 digest for each stored fixture or archive;
- secret-scan confirmation after sanitization;
- expected normalized finding or evidence coverage;
- known unsupported fields and acceptable lossy mappings;
- redaction expectations for report, UI, PFF, and handoff outputs;
- composition expectations with nmap, nuclei, Burp, ZAP, Nessus, SARIF, ffuf,
  sqlmap, Metasploit, and neutral C2 evidence.

## Implementation Gate

An adapter may move from intake to implementation only when:

- the fixture is real exported tool output from an authorized target;
- provenance and sanitization notes are committed with the fixture;
- the candidate improves the local report or handoff workflow;
- the parser can preserve the raw source under `raw/<tool>/`;
- unsupported data can be warned about instead of silently discarded;
- tests can cover a valid real fixture and malformed negative fixtures.

Synthetic or hand-authored files are allowed only for negative tests. They must
not be used to claim adapter support.

## Review Checklist

Before merging an adapter, verify:

- raw evidence paths stay inside the workspace;
- finding IDs are stable and deterministic;
- source digests and locators survive normalization;
- severity, confidence, assets, services, URLs, references, and evidence are
  documented;
- sensitive values are redacted before reports, PFF exports, and handoff drafts;
- parser warnings are visible in CLI JSON output;
- docs state import-only behavior and do not imply scanning, exploitation, live credential validation, or target interaction.

## Current Parked Candidates

Current candidates remain parked until their own intake records satisfy this
gate:

1. BloodHound collection exports.
2. NetExec or CrackMapExec output.
3. Trivy filesystem or container JSON.
4. Lynis report output.
5. osquery JSON results.
6. OpenSCAP ARF/XML.
