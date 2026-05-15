# Privacy And Data Handling

Piranesi is local-first by default. Deterministic assessment reads local evidence,
writes local artifacts, and does not upload source code, host evidence, reports,
or snapshots unless you explicitly enable an external path such as LLM analysis or
an outbound exporter.

## Local Storage

Piranesi writes data where you ask it to write data:

| Workflow | Default or typical path | Contents |
| --- | --- | --- |
| Demo | `piranesi-demo-output/` | `host-report.json`, `host-report.md`, and optional rendered outputs. |
| Host collection | `piranesi-evidence/` | `host_snapshot.json`, `collection-manifest.json`, and raw osquery, Trivy, command, Lynis, or OpenSCAP evidence. |
| Host assessment | `piranesi-output/` | Host reports, optional PDF/static dashboard, policy results, and derived evidence summaries. |
| Source-code run | `.piranesi-out/...` or the supplied `--output` | Stage artifacts such as `scan.json`, `detect.json`, `verify.json`, `legal.json`, `patch.json`, `report.json`, and `report.md`. |
| Local UI report review | The report directory you pass to `piranesi ui` | The UI reads existing report files and serves embedded assets plus redacted API responses. |
| ZIP workbench | `~/.piranesi/ui-jobs/<job-id>/` unless `--jobs-dir` is supplied | `upload.zip`, extracted source, generated report output, and `scan.log`. |

Piranesi does not automatically prune workbench job directories. Delete old jobs
from `~/.piranesi/ui-jobs/` or point `--jobs-dir` at a temporary directory when
you want short-lived storage. The UI does not expose the report directory as a
general file server.

Trace logging writes to `.piranesi-trace.jsonl` by default when enabled. Prompt
logging is disabled by default (`trace.log_prompts = false`).

## Uploaded ZIPs

`piranesi ui --workbench` accepts ZIP uploads for local application review. The
server:

- binds to `127.0.0.1` by default
- rejects unsafe archive paths and symlinks
- extracts into the configured local jobs directory
- runs the compatibility source-code pipeline with `--no-execute`
- removes API-key-like environment variables from the scan subprocess
- writes the generated report beside the extracted job files

The uploaded ZIP and extracted source remain on disk until you delete the job
directory.

## Host Evidence

Host collection is read-only. It runs osquery queries and optional local commands
for firewall, update, SSH, group, sysctl, Lynis, and OpenSCAP evidence. Failed or
permission-limited commands are recorded in `collection-manifest.json`.

Authentication/session evidence is opt-in with `--auth-evidence`. When enabled,
Piranesi collects bounded auth/session summaries and redacts obvious secrets
before writing them to the bundle.

## LLM Usage

Deterministic host assessment and deterministic source-code reporting do not need
LLM credentials. LLM-backed paths only run when credentials and options are
present, such as host `--analysis llm|both`, source-code model-assisted triage,
patch generation, legal memo drafting, or hypothesis generation.

Host LLM analysis uses a structured redaction layer before provider calls.
Hostnames, usernames, IP addresses, MAC addresses, home paths, command lines, and
likely secrets are replaced with placeholders while package and service names are
preserved. Non-host LLM redaction is best-effort and intentionally documented as a
known limitation. If policy requires zero outbound model data, leave LLM provider
credentials unset and use deterministic mode.

## Outbound Network Calls

External network activity is opt-in or tied to explicit data-refresh commands:

- `piranesi advisory update` contacts configured advisory sources.
- LLM-backed stages call the configured LiteLLM-compatible provider.
- `piranesi export webhook --send --yes` sends a redacted webhook payload.
- `piranesi export github-issues --create --yes` creates GitHub issues with `GITHUB_TOKEN`.
- `piranesi export jira --create --yes` creates Jira tickets with Jira credentials.
- Verification sandbox networking is disabled by default through `sandbox.network_enabled = false`.

Exporters are dry-run by default. Raw host snapshots are excluded from outbound
payloads unless you explicitly pass `--include-raw-snapshot`, and sensitive host
metadata is redacted unless you explicitly pass `--no-redact`.

## Redaction Limits

Redaction is a safety control, not a guarantee. It is designed to catch common
host identifiers, usernames, IPs, MAC addresses, home paths, command lines,
authorization headers, cookies, API keys, tokens, passwords, and private-key
blocks. It can miss project-specific secrets, unusual encodings, binary data,
screenshots, generated logs, or values embedded in unexpected formats.

For strict data-boundary environments:

- run deterministic mode
- keep LLM credentials unset
- keep exporters in dry-run mode
- review generated artifacts before sharing
- delete workbench job directories after use
- avoid `--no-redact` and `--include-raw-snapshot`
