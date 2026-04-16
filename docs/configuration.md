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
| `max_tokens` | `int` | `500000` | Budget metadata. Present in config, but not yet enforced as a hard token cap everywhere. |

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
- `--format`
- `--config`
- `--output`
- `--trace`

For the authoritative option list, run:

```bash
uv run piranesi run --help
```
