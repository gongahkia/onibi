> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Configuration

Piranesi loads configuration from `piranesi.toml`, then applies environment-variable overrides, then applies CLI overrides. The effective precedence is:

1. Built-in defaults in `src/piranesi/config.py`
2. `piranesi.toml`
3. `PIRANESI_*` environment variables
4. CLI flags such as `--timeout` or `--output`

## Canonical `piranesi.toml`

```toml
[models]
scanner = "gpt-4o-mini"
detector = "gpt-4o-mini"
triage = "gpt-4o"
patcher = "claude-sonnet-4-20250514"

[models_fallback]
default = "gpt-4o-mini"

[budget]
max_cost_usd = 5.0
warn_at_usd = 3.0
max_tokens = 500000

[sandbox]
docker_image = "piranesi-sandbox:latest"
timeout_seconds = 30
network_enabled = false

[output]
format = "both"
output_dir = "./piranesi-output"

[trace]
enabled = true
file_path = ".piranesi-trace.jsonl"
log_prompts = false

[joern]
binary_path = "joern"
server_port = 8080
startup_timeout_seconds = 30
query_timeout_seconds = 60
jvm_memory = "2g"

[scan]
include_patterns = ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"]
exclude_patterns = ["**/node_modules/**", "**/dist/**", "**/*.d.ts"]
max_file_size = 1048576

[scan.custom_sources]
patterns = []
source_type = "custom"

[scan.custom_sinks]
patterns = []
sink_type = "custom"
cwe_id = "CWE-89"
include_receivers = []
exclude_receivers = []

[verify]
proof_mode = "safe"
target_profile = "express_local"

[verify.target_profiles.express_local]
command = "npm run dev"
cwd = "examples/vuln-express"
startup_timeout_seconds = 45
readiness_url = "/health"
base_url = "http://127.0.0.1:{port}"
teardown = "on_success"
logs_path = "./piranesi-output/verify-launch.log"

[verify.target_profiles.express_local.env]
PORT = "4010"

[rollout]
environment = "staging"
policy_profile = "staging_guardrails"

[rollout.policy_profiles.staging_guardrails]
verify_proof_mode = "safe"
verify_target_profile = "express_local"
max_cost_usd = 3.0
max_tokens = 200000
trace_log_prompts = false
suppression_fail_on_invalid = true
suppression_fail_on_expired = true
suppression_fail_on_stale = true
allowed_models = ["gpt-4o-mini", "gpt-4o"]

[ownership]
service = "checkout-api"
system = "payments-platform"
team = "payments-eng"
owner = "payments-oncall"
repository = "acme/checkout"
environment = "production"
control_owner = "grc-core"
autodetect_repository = true
autodetect_service = true

[[ownership.path_mappings]]
path = "src/routes/payments/**"
team = "payments-routing"
owner = "payments-route-owner"

[[ownership.package_mappings]]
package = "@acme/identity"
team = "identity-eng"
owner = "identity-oncall"
control_owner = "identity-grc"

[[ownership.control_mappings]]
framework = "SOC2"
control = "CC6.6"
owner = "soc-controls-team"
```

Notes:

- The repository root `piranesi.toml` is a sample, not a schema. The canonical schema lives in `src/piranesi/config.py`.
- Legacy aliases `[models.budget]` and `[models.fallback]` are still normalized into `[budget]` and `[models_fallback]` for compatibility.
- TOML does not have a native `null` literal. To leave an optional field unset, omit it from the file.

## Section Reference

### `[models]`

Primary model routing for LLM-backed stages.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `scanner` | `str` | `gpt-4o-mini` | Reserved scanner-stage model selection. |
| `detector` | `str` | `gpt-4o-mini` | Reserved detector-stage model selection. |
| `triage` | `str` | `gpt-4o` | Model used by triage. |
| `skeptic` | `str | null` | `null` | Optional second model for skeptical review during triage. |
| `patcher` | `str` | `claude-sonnet-4-20250514` | Model used for patch generation. |

### `[models_fallback]`

Fallback models used when the primary model fails.

| Key | Type | Default |
| --- | --- | --- |
| `default` | `str | null` | `null` |
| `scanner` | `str | null` | `null` |
| `detector` | `str | null` | `null` |
| `triage` | `str | null` | `null` |
| `skeptic` | `str | null` | `null` |
| `patcher` | `str | null` | `null` |

If a stage-specific fallback is unset, Piranesi falls back to `models_fallback.default`.

LLM credentials are optional for deterministic scan/detect/report operation. Without a LiteLLM-compatible API key, triage preserves reachable static findings and patch generation is skipped. Set a provider key when you want model-assisted false-positive discrimination, patch generation, or legal memo drafting.

### `[budget]`

LLM budget controls.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `max_cost_usd` | `float` | `5.0` | Hard spend ceiling enforced by the model router and trace writer. |
| `warn_at_usd` | `float | null` | `null` | Optional warning threshold before `max_cost_usd` is reached. |
| `max_tokens` | `int` | `500000` | Hard token cap for LLM stages using local token estimation. Piranesi clamps completion tokens, truncates oversized prompt context, and degrades triage/patch gracefully when the budget is exhausted. |

When token constraints affect a call, Piranesi logs `llm_token_budget_adjusted` warnings so you can see when context was omitted or completion tokens were clamped.

### `[sandbox]`

Docker verification settings.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `docker_image` | `str` | `piranesi-sandbox:latest` | Base image name used when building the verification container. |
| `timeout_seconds` | `int` | `30` | Max wait for target startup and exploit execution. |
| `network_enabled` | `bool` | `false` | Sandbox network toggle. Keep this `false` unless you intentionally want egress during verification. |

### `[output]`

Combined report settings.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `format` | `str` | `both` | Report format selector. Supported values include `json`, `markdown`, `both`, `sarif`, `junit`, `csv`, `tui`, and `compliance`. |
| `output_dir` | `str` | `./piranesi-output` | Directory for stage artifacts and rendered reports. |

### `[verify]`

Verification proof-mode controls.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `proof_mode` | `"safe" \| "unsafe"` | `safe` | `safe` prefers non-mutating probes and excludes destructive templates. `unsafe` explicitly opts in to higher-risk templates (for example mutation-oriented probes) and should only be used in disposable or authorized environments. |
| `target_profile` | `str \| null` | `null` | Optional profile name to use for verification app startup/readiness. |
| `target_profiles` | `table` | `{}` | Named reusable launch profiles keyed by profile name. |

### `[verify.target_profiles.<name>]`

Reusable target launch profile used by `verify` or `run` when `verify.target_profile` or `--target-profile` is set.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `command` | `str \| null` | `null` | Optional command to start the target process (for example `npm run dev` or `uvicorn app:app --port 8000`). |
| `cwd` | `str \| null` | `null` | Optional working directory for `command`. Relative paths are resolved from the scan target directory. |
| `env` | `table` | `{}` | Environment variable overrides for target startup/readiness. |
| `startup_timeout_seconds` | `int` | `30` | Max time to wait for readiness checks to pass. |
| `readiness_url` | `str \| null` | `null` | Optional readiness endpoint path/URL. Defaults to `/`. |
| `readiness_command` | `str \| null` | `null` | Optional command-based readiness probe. When set, this is polled until success or timeout. |
| `base_url` | `str \| null` | `null` | Base URL used for verification requests. Supports `{port}` placeholder from profile env `PORT`. |
| `teardown` | `"always" \| "on_success" \| "never"` | `always` | Controls whether the launched process is terminated after verification. |
| `logs_path` | `str \| null` | `null` | Optional path for persisted startup/runtime logs. Included in `verify.json` attempts when set. |

### `[rollout]`

Environment-aware policy selection for operational rollout controls.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `environment` | `"dev" \| "staging" \| "prod" \| null` | `null` | Environment label used by policy and governance tooling. |
| `policy_profile` | `str \| null` | `null` | Optional selected profile key from `[rollout.policy_profiles]`. |
| `policy_profiles` | `table` | `{}` | Named profile definitions that can lock verification and LLM controls. |

### `[rollout.policy_profiles.<name>]`

Reusable rollout policy profile for environment-specific controls.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `verify_proof_mode` | `"safe" \| "unsafe" \| null` | `null` | Overrides `verify.proof_mode` when profile is selected. |
| `verify_target_profile` | `str \| null` | `null` | Overrides `verify.target_profile` when profile is selected. |
| `max_cost_usd` | `float \| null` | `null` | Overrides `budget.max_cost_usd` to cap LLM spend per run. |
| `max_tokens` | `int \| null` | `null` | Overrides `budget.max_tokens` for token budget control. |
| `trace_log_prompts` | `bool \| null` | `null` | Overrides `trace.log_prompts`. Keep `false` in production unless explicitly approved. |
| `suppression_fail_on_invalid` | `bool \| null` | `null` | Overrides `suppression.fail_on_invalid`. |
| `suppression_fail_on_expired` | `bool \| null` | `null` | Overrides `suppression.fail_on_expired`. |
| `suppression_fail_on_stale` | `bool \| null` | `null` | Overrides `suppression.fail_on_stale`. |
| `allowed_models` | `list[str]` | `[]` | If non-empty, configured models must all be in this allowlist or config load fails. |

### `[ownership]`

Optional ownership metadata used to attribute findings to services, systems, teams, and control owners. This section is optional and does not affect scan execution when omitted.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `service` | `str \| null` | `null` | Default service name attached to findings. |
| `system` | `str \| null` | `null` | Default system or platform name attached to findings. |
| `team` | `str \| null` | `null` | Default engineering team for findings. |
| `owner` | `str \| null` | `null` | Default owner (team alias or individual) for findings. |
| `repository` | `str \| null` | `null` | Default repository label. |
| `environment` | `str \| null` | `null` | Deployment environment label such as `dev`, `staging`, or `production`. |
| `control_owner` | `str \| null` | `null` | Default control owner used in report/compliance metadata. |
| `autodetect_repository` | `bool` | `true` | When true, infer repository from the scan target path if `repository` is not set. |
| `autodetect_service` | `bool` | `true` | When true, infer service from repository/path if `service` is not set. |
| `path_mappings` | `list[table]` | `[]` | Ordered overrides matching source/sink paths by glob. |
| `package_mappings` | `list[table]` | `[]` | Ordered overrides matching package metadata (`package`, `source_package`, `sink_package`). |
| `control_mappings` | `list[table]` | `[]` | Explicit framework/control owner assignments. |

### `[[ownership.path_mappings]]`

Per-path ownership override. The most specific matching pattern wins.

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `path` | `str` | yes | Glob pattern applied to source/sink file paths (for example `src/routes/payments/**`). |
| `service`/`system`/`team`/`owner`/`repository`/`environment`/`control_owner` | `str \| null` | no | Field overrides applied when the path matches. |

### `[[ownership.package_mappings]]`

Per-package ownership override. Later matching entries override earlier entries.

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `package` | `str` | yes | Package identifier matched against finding package metadata. |
| `service`/`system`/`team`/`owner`/`repository`/`environment`/`control_owner` | `str \| null` | no | Field overrides applied when the package matches. |

### `[[ownership.control_mappings]]`

Control owner assignments exported in `report.json` and compliance bundle metadata.

| Key | Type | Required | Meaning |
| --- | --- | --- | --- |
| `framework` | `str` | yes | Framework key (for example `SOC2`, `PCI_DSS`). |
| `control` | `str` | yes | Control reference (for example `CC6.6`). |
| `owner` | `str` | yes | Control owner label. |

### `[trace]`

LLM trace logging.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `enabled` | `bool` | `true` | Enables `.jsonl` trace output. |
| `file_path` | `str` | `.piranesi-trace.jsonl` | Trace file destination. |
| `log_prompts` | `bool` | `false` | When `true`, stores raw prompts and responses. Leave `false` unless you have a reason to retain that material. |

### `[joern]`

Joern runtime configuration.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `binary_path` | `str` | `joern` | Joern executable path. |
| `server_port` | `int` | `8080` | Preferred starting port. Piranesi will walk to the next free port on conflict. |
| `startup_timeout_seconds` | `int` | `30` | Wait time for the Joern server to become ready. |
| `query_timeout_seconds` | `int` | `60` | HTTP timeout for individual Joern queries. |
| `jvm_memory` | `str` | `2g` | JVM heap flag passed as `-J-Xmx...`. |

### `[scan]`

Source inclusion and built-in taint surface controls.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `include_patterns` | `list[str]` | `["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"]` | Files eligible for transpilation and analysis. |
| `exclude_patterns` | `list[str]` | `["**/node_modules/**", "**/dist/**", "**/*.d.ts"]` | Files removed before analysis. |
| `max_file_size` | `int` | `1048576` | Maximum file size in bytes. |

### `[scan.custom_sources]`

Additional Joern source expressions appended to the built-in source list.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `patterns` | `list[str]` | `[]` | Raw CPGQL expressions that identify source nodes. |
| `source_type` | `str` | `custom` | Label assigned to matching sources. Recognized values include `request_body`, `request_param`, `header`, `cookie`, `env_var`, `url_param`, and `custom`. Unknown values normalize to `custom`. |

### `[scan.custom_sinks]`

Additional Joern sink expressions appended to the built-in sink list.

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `patterns` | `list[str]` | `[]` | Raw CPGQL expressions that identify sink nodes. |
| `sink_type` | `str` | `custom` | Sink label. Recognized values include `sql_query`, `shell_exec`, `eval`, `html_output`, `file_read`, `file_write`, `http_request`, and `custom`. Unknown values normalize to `custom`. |
| `cwe_id` | `str | null` | `null` | CWE tag attached to findings that use the custom sink. |
| `include_receivers` | `list[str]` | `[]` | Optional receiver allowlist. A sink call must use one of these receivers (exact or dotted-prefix match, such as `axios` matching `axios.get`). |
| `exclude_receivers` | `list[str]` | `[]` | Optional receiver denylist. Matching sink calls are dropped when the receiver is listed (exact or dotted-prefix match, such as `app` matching `app.get`). |

## Built-In Source and Sink Coverage

The current built-in source specs cover:

- `req.body.*`
- `req.query.*`
- `req.params.*`
- `req.headers.*`
- `req.cookies.*`
- `process.env.*`
- `new URL(...)` and `new URLSearchParams(...)`

The current built-in sink specs cover:

- `query|$queryRaw|$executeRaw|raw`
- `exec|execSync`
- `spawn|spawnSync`
- `eval|Function`
- `dangerouslySetInnerHTML`
- `send|render|write`
- `readFile|readFileSync`
- `writeFile|writeFileSync`
- `fetch|get|post|request`

This matters when you interpret misses. The planted SQLi miss in `examples/vuln-express` happened because the app used a local helper named `query()` rather than a modeled database call.

## Environment Variable Overrides

Every leaf config field can be overridden with an uppercase `PIRANESI_...` environment variable. Dots become underscores.

Examples:

```bash
export PIRANESI_OUTPUT_OUTPUT_DIR=.piranesi-out/release
export PIRANESI_SANDBOX_TIMEOUT_SECONDS=60
export PIRANESI_JOERN_JVM_MEMORY=4g
export PIRANESI_SCAN_INCLUDE_PATTERNS='["src/**/*.ts","src/**/*.js"]'
export PIRANESI_SCAN_EXCLUDE_PATTERNS='**/node_modules/**,**/dist/**'
```

Rules:

- Booleans accept `1`, `true`, `yes`, `on`, `0`, `false`, `no`, and `off`.
- Lists accept either JSON arrays or comma-separated strings.
- CLI flags still win over environment variables.

## CLI Overrides

The current `run` command can override these config fields directly:

- `--include` and `--exclude`
- `--triage-model`
- `--patch-model`
- `--docker-image`
- `--timeout`
- `--proof-mode`
- `--target-profile`
- `--format`
- `--config`
- `--output`
- `--trace`

For the authoritative option list, run:

```bash
uv run piranesi run --help
```
