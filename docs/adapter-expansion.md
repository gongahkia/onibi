# Phase 1.1 Adapter Expansion

Phase 1.1 expands import coverage after the Phase 1 workflow has proven the common model with
nmap, nuclei, and Burp Suite Pro exports. Adapters remain import-only: they parse operator-supplied
exports, preserve raw evidence, normalize findings, and keep provenance intact.

## Selection Order

The first implementation issues should be split in this order:

1. OWASP ZAP alerts and report exports.
2. Nessus `.nessus` exports.
3. SARIF findings.
4. ffuf JSON output.
5. sqlmap session and output artifacts.
6. Metasploit loot and session evidence.

ZAP is first because it is open source, easy to regenerate in a local authorized lab, and closest to
the existing Burp/nuclei web-finding model. Nessus is second because it is common in consultancy
deliverables, but it must wait for sanitized real `.nessus` exports with documented provenance.
SARIF is third because it is useful as an interchange bridge, but it should align with the public
PFF direction before it becomes the main adapter path.

## Real Fixture Gate

No adapter should merge without real exported tool output. Each fixture must include:

- tool name, version, and export command or UI path;
- target type and authorization note;
- sanitization notes;
- SHA-256 digest of the stored fixture;
- expected parser coverage and known unsupported fields;
- secret-scanning confirmation.

Synthetic or hand-authored files may be used only as negative parser tests and must not be used to
claim adapter support.

## Adapter Checklist

Each adapter implementation issue should cover:

- supported input format and minimum tested tool version;
- raw evidence preservation path under `raw/<tool>/`;
- severity, confidence, asset, service, URL/path, CWE/CVE/reference, and evidence mapping;
- deduplication key and stable finding ID rules;
- redaction behavior for request/response, payload, credential, and client-specific fields;
- parser warnings for unsupported or lossy fields;
- capability documentation;
- tests for valid real fixtures, malformed input, provenance metadata, and multi-tool workspace
  composition.

## Child Issue Template

Use this template for each adapter-specific GitHub issue:

```markdown
## Goal
Import <tool> exports as preserved raw evidence and normalized Piranesi findings.

## Supported inputs
- <format and version>

## Fixture requirements
- Real exported fixture from <tool/version>.
- Provenance and sanitization notes.
- Secret scan passes.

## Acceptance criteria
- Raw export is copied under `raw/<tool>/`.
- Findings preserve source digests and locators.
- Severity/confidence/assets/references are mapped and documented.
- Unsupported fields produce warnings instead of silent loss.
- Parser tests use real fixtures and malformed-input fixtures.
- Adapter composes with existing nmap/nuclei/Burp findings in one workspace.

## Out of scope
- Active scanning.
- Authenticated target orchestration.
- Synthetic fixtures as support proof.
```
