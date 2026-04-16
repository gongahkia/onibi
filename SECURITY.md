# Security Policy

## Supported Versions

Only the latest `0.2.x` release line is supported for security fixes.

| Version | Supported |
| --- | --- |
| `0.2.x` | Yes |
| `< 0.2.0` | No |

## Reporting a Vulnerability

Do not open a public GitHub issue for vulnerabilities in Piranesi itself.

Use GitHub Security Advisories for this repository:

- <https://github.com/gongahkia/piranesi/security/advisories/new>

If GitHub Advisories are unavailable for you, contact the maintainer privately using the contact information published on the repository profile.

## What To Include

Please include:

- A clear description of the issue
- Affected version or commit
- Reproduction steps or a proof of concept
- Expected impact
- Any suggested fix or mitigation, if you have one

## Response Targets

Piranesi follows these target timelines for reports about Piranesi itself:

- Acknowledge receipt within 48 hours
- Complete initial triage within 7 days
- Request 90 days of coordinated disclosure from the initial report date before public disclosure

These are targets, not guarantees, but the project aims to meet them for every valid report.

## Scope

In scope:

- Vulnerabilities in Piranesi's own code
- Sandbox escapes or privilege-boundary failures during the verify stage
- Sensitive-data leaks in reports, traces, or generated artifacts
- Cases where scanning untrusted code can compromise the host running Piranesi

Out of scope:

- Vulnerabilities in the target application being scanned
- Vulnerabilities that only affect third-party dependencies and should be reported upstream
- Feature requests, correctness bugs, or false positives that do not create a security impact in Piranesi itself

## Handling and Disclosure

For valid issues, the expected process is:

1. Confirm impact and affected versions during triage.
2. Develop and test a fix.
3. Coordinate a release.
4. Publish an advisory after the fix is available or after the 90-day window expires.

Reporter credit will be given in release notes unless you ask to remain anonymous.

## Hardening Notes

Piranesi processes untrusted source code and should be treated as security-sensitive software. Current risk areas include:

- Parser or Joern-driven crashes on malicious inputs
- Prompt injection attempts embedded in source comments when LLM-backed stages are enabled
- Sandbox escape attempts during verification
- Resource exhaustion from pathological projects

If your report relates to one of these areas, say so explicitly in the advisory so it can be triaged faster.
