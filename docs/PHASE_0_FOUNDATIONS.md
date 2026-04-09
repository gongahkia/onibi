# Phase 0: Foundations

## 1. Phase Overview

Phase 0 establishes the project skeleton: build system, CLI entry point, configuration management, tracing, logging, testing infrastructure, and CI. No analysis logic is implemented here. Every subsequent phase (1-6) plugs into the scaffolding created in this phase.

This phase **blocks all other phases** and is **blocked by nothing**. It can start immediately.

The goal is a fully runnable CLI binary (`piranesi`) that accepts all subcommands, loads configuration, initializes tracing, and exits with a "not implemented" message for each pipeline stage. By the end of this phase, `uv run piranesi run .` should parse config, open a trace file, print "not implemented" for the scan stage, and exit cleanly.

---

## 2. Repository Scaffolding

**Effort estimate: 6-8 ideal hours**

### 2.1 pyproject.toml

```toml
[project]
name = "piranesi"
version = "0.1.0"
description = "CLI-native cybersecurity analysis tool for TypeScript/JavaScript source code"
readme = "README.md"
license = { text = "MIT" }
requires-python = ">=3.12"
authors = [{ name = "piranesi contributors" }]
dependencies = [
    "typer>=0.12.0",
    "rich>=13.7.0",
    "pydantic>=2.6.0",
    "z3-solver>=4.12.0",
    "litellm>=1.30.0",
    "docker>=7.0.0",
    "tomli>=2.0.0; python_version < '3.11'",
]
# NOTE: Joern (JVM-based CPG engine) is a runtime dependency but NOT a pip package.
# It is installed separately via: brew install joern (macOS) or binary release.
# Requires JVM 11+: brew install openjdk@11
# tree-sitter is NOT needed — Joern handles parsing and data flow analysis.

[project.scripts]
piranesi = "piranesi.cli:app"

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.hatch.build.targets.wheel]
packages = ["src/piranesi"]

[tool.uv]
dev-dependencies = [
    "pytest>=8.0.0",
    "pytest-cov>=5.0.0",
    "pytest-asyncio>=0.23.0",
    "mypy>=1.9.0",
    "ruff>=0.3.0",
    "types-docker>=7.0.0",
]

[tool.pytest.ini_options]
testpaths = ["tests"]
pythonpath = ["src"]
addopts = "-v --tb=short --strict-markers"
markers = [
    "slow: marks tests as slow (deselect with '-m \"not slow\"')",
    "integration: marks integration tests requiring external services",
]

[tool.mypy]
python_version = "3.12"
strict = true
warn_return_any = true
warn_unused_configs = true
disallow_untyped_defs = true
disallow_incomplete_defs = true
check_untyped_defs = true
disallow_any_generics = true
no_implicit_optional = true
warn_redundant_casts = true
warn_unused_ignores = true
show_error_codes = true
namespace_packages = true
explicit_package_bases = true
mypy_path = "src"

[tool.coverage.run]
source = ["piranesi"]
omit = ["tests/*"]

[tool.coverage.report]
fail_under = 60
show_missing = true
```

### 2.2 ruff.toml

```toml
target-version = "py312"
line-length = 100
src = ["src"]

[lint]
select = [
    "E",    # pycodestyle errors
    "W",    # pycodestyle warnings
    "F",    # pyflakes
    "I",    # isort
    "B",    # flake8-bugbear
    "C4",   # flake8-comprehensions
    "UP",   # pyupgrade
    "SIM",  # flake8-simplify
    "S",    # flake8-bandit (security)
    "RUF",  # ruff-specific
]
ignore = [
    "S101",  # assert used (needed in tests)
    "S603",  # subprocess call (needed in sandbox)
    "S607",  # start process with partial path
]

[lint.isort]
known-first-party = ["piranesi"]

[format]
quote-style = "double"
indent-style = "space"
```

### 2.3 .gitignore

Standard Python .gitignore plus:
- `.venv/`, `__pycache__/`, `*.pyc`, `dist/`, `*.egg-info/`
- `.piranesi-trace*.jsonl` (trace files)
- `.env` (API keys)
- `piranesi-output/` (default output dir)
- `.mypy_cache/`, `.ruff_cache/`, `.pytest_cache/`

### 2.4 Directory Structure

Create all directories and `__init__.py` files as specified in the project structure. The root `__init__.py` exports the version string:

```python
__version__ = "0.1.0"
```

Each sub-package (`models/`, `scan/`, `detect/`, `triage/`, `verify/`, `legal/`, `patch/`, `report/`, `llm/`) gets an empty `__init__.py`.

### 2.5 Tasks

| # | Task | Acceptance |
|---|------|------------|
| S1 | Write `pyproject.toml` | `uv sync` succeeds, all deps install |
| S2 | Write `ruff.toml` | `uv run ruff check src/` passes on empty files |
| S3 | Write `.gitignore` | `git status` shows clean after build artifacts |
| S4 | Create all dirs + `__init__.py` files | `from piranesi import __version__` works |
| S5 | Verify `uv build` | Wheel builds successfully |

---

## 3. CLI Skeleton

**Effort estimate: 8-10 ideal hours**

### 3.1 Architecture

`src/piranesi/cli.py` defines a `typer.Typer` app with 8 subcommands. Each subcommand is a thin wrapper that:
1. Loads config (via `config.py`)
2. Initializes tracing (via `trace.py`)
3. Validates the `--authorized` flag
4. Prints "not implemented" and exits with code 1

### 3.2 Global Options

```
--config, -c       Path to piranesi.toml (default: ./piranesi.toml)
--output, -o       Output directory (default: ./piranesi-output)
--verbose, -v      Enable verbose logging (DEBUG level)
--trace            Trace file path (default: .piranesi-trace.jsonl)
--authorized       Required flag: confirms authorization to test target code
```

### 3.3 The --authorized Flag

This is a safety gate. Piranesi generates real exploits. Running it against code you don't own or have permission to test is irresponsible at best, illegal at worst.

Behavior:
- If `--authorized` is **not** present: print a warning explaining the requirement, then exit with code 2.
- If `--authorized` **is** present: print a confirmation prompt:
  ```
  [WARNING] Piranesi generates real exploits against the target codebase.
  You must have explicit authorization to test this code.
  Do you confirm you are authorized? [y/N]:
  ```
  User must type `y` or `yes` (case-insensitive). Any other input exits with code 2.
- For CI/scripted usage: `--authorized --yes` skips the interactive prompt (assumes confirmation). Both flags must be present.

### 3.4 Subcommands

```
piranesi scan <target_dir>
    Parse TS/JS files, build IR and call graph.
    Options: --include, --exclude (glob patterns)

piranesi detect <target_dir>
    Run taint analysis on parsed IR.
    Options: --sources (custom sources file), --sinks (custom sinks file)

piranesi triage <findings_file>
    LLM-based triage to filter false positives.
    Options: --model (override triage model), --threshold (confidence threshold 0.0-1.0)

piranesi verify <findings_file>
    Generate and execute exploits in sandbox.
    Options: --docker-image, --timeout, --no-execute (generate only, don't run)

piranesi legal <findings_file>
    Map findings to regulatory obligations.
    Options: --frameworks (e.g., "gdpr,pci-dss,sox"), --jurisdiction

piranesi patch <findings_file>
    Generate patch suggestions via LLM.
    Options: --model (override patch model), --apply (auto-apply patches)

piranesi report <findings_file>
    Render final report.
    Options: --format (json|markdown|html|sarif), --template

piranesi run <target_dir>
    Run full pipeline (scan → detect → triage → verify → legal → patch → report).
    Accepts all options from individual stages.
```

### 3.5 Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success, no findings |
| 1 | Success, findings present |
| 2 | Authorization failure |
| 3 | Configuration error |
| 4 | Runtime error (crash) |
| 5 | Sandbox error |

### 3.6 Tasks

| # | Task | Acceptance |
|---|------|------------|
| C1 | Create `cli.py` with typer app + all subcommands stubbed | `piranesi --help` shows all 8 commands |
| C2 | Implement `--authorized` gate | Running without `--authorized` exits 2 |
| C3 | Wire global options | `--config`, `--verbose`, `--trace` parsed correctly |
| C4 | Add `--yes` flag for non-interactive mode | `--authorized --yes` skips prompt |
| C5 | Exit code handling | Each stub exits with code 1 |

---

## 4. Configuration System

**Effort estimate: 6-8 ideal hours**

### 4.1 Config Loading

Load order (later overrides earlier):
1. Built-in defaults (hardcoded in the Pydantic model)
2. `piranesi.toml` at the path specified by `--config` (or `./piranesi.toml`)
3. Environment variables (`PIRANESI_*`)

### 4.2 piranesi.toml Example

```toml
[models]
scanner = "gpt-4o-mini"
detector = "gpt-4o-mini"
triage = "gpt-4o"
patcher = "claude-sonnet-4-20250514"
legal_memo = "claude-sonnet-4-20250514"

[models.budget]
max_cost_usd = 5.00
max_tokens = 500000

[sandbox]
docker_image = "piranesi-sandbox:latest"
timeout_seconds = 30
network_enabled = false

[output]
format = "both"       # json | markdown | both
output_dir = "./piranesi-output"

[trace]
enabled = true
file_path = ".piranesi-trace.jsonl"
log_prompts = false   # SECURITY: never enable in CI or with shared trace files

[scan]
include_patterns = ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"]
exclude_patterns = ["**/node_modules/**", "**/dist/**", "**/*.d.ts", "**/*.test.ts", "**/*.spec.ts"]
max_file_size = 1048576  # 1MB
```

### 4.3 Pydantic Config Model

```python
from pydantic import BaseModel, Field

class ModelsConfig(BaseModel):
    scanner: str = "gpt-4o-mini"
    detector: str = "gpt-4o-mini"
    triage: str = "gpt-4o"
    patcher: str = "claude-sonnet-4-20250514"
    legal_memo: str = "claude-sonnet-4-20250514"

class BudgetConfig(BaseModel):
    max_cost_usd: float = 5.0
    max_tokens: int = 500_000

class SandboxConfig(BaseModel):
    docker_image: str = "piranesi-sandbox:latest"
    timeout_seconds: int = 30
    network_enabled: bool = False

class OutputConfig(BaseModel):
    format: str = "both"
    output_dir: str = "./piranesi-output"

class TraceConfig(BaseModel):
    enabled: bool = True
    file_path: str = ".piranesi-trace.jsonl"
    log_prompts: bool = False

class ScanConfig(BaseModel):
    include_patterns: list[str] = Field(
        default=["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"]
    )
    exclude_patterns: list[str] = Field(
        default=["**/node_modules/**", "**/dist/**", "**/*.d.ts"]
    )
    max_file_size: int = 1_048_576

class PiranesiConfig(BaseModel):
    models: ModelsConfig = ModelsConfig()
    budget: BudgetConfig = BudgetConfig()
    sandbox: SandboxConfig = SandboxConfig()
    output: OutputConfig = OutputConfig()
    trace: TraceConfig = TraceConfig()
    scan: ScanConfig = ScanConfig()
```

### 4.4 Environment Variable Overrides

Pattern: `PIRANESI_<SECTION>_<KEY>`. Examples:
- `PIRANESI_MODELS_SCANNER=gpt-4o` overrides `models.scanner`
- `PIRANESI_SANDBOX_TIMEOUT_SECONDS=60` overrides `sandbox.timeout_seconds`
- `PIRANESI_TRACE_LOG_PROMPTS=true` overrides `trace.log_prompts`

Implementation: after loading TOML and constructing the Pydantic model, iterate over all fields and check for corresponding env vars. Apply overrides before validation.

### 4.5 Tasks

| # | Task | Acceptance |
|---|------|------------|
| CF1 | Define Pydantic config models in `config.py` | Models instantiate with defaults, pass mypy strict |
| CF2 | Implement TOML loading via `tomllib` | Config loads from file, validates, returns typed model |
| CF3 | Implement env var override layer | Setting `PIRANESI_MODELS_SCANNER=x` overrides scanner |
| CF4 | Write default `piranesi.toml` at project root | File parses correctly |
| CF5 | Error handling: missing file, invalid TOML, bad values | Clear error messages, exit code 3 |

---

## 5. Trace Logging Infrastructure

**Effort estimate: 6-8 ideal hours**

### 5.1 TraceEntry Model

Every LLM interaction produces a `TraceEntry`:

```python
from pydantic import BaseModel
from datetime import datetime

class TraceEntry(BaseModel):
    timestamp: str          # ISO 8601 (e.g., "2026-04-09T14:23:01.442Z")
    stage: str              # pipeline stage (scan, detect, triage, verify, legal, patch)
    model: str              # model identifier (e.g., "gpt-4o")
    prompt_hash: str        # SHA-256 hex digest of the prompt
    response_hash: str      # SHA-256 hex digest of the response
    prompt_tokens: int      # token count from LLM response metadata
    response_tokens: int
    cost_usd: float         # computed from token counts + model pricing
    duration_ms: int        # wall-clock time of the LLM call
    cache_hit: bool         # whether LiteLLM cache was used
    prompt: str | None = None      # full prompt text (only if trace.log_prompts = true)
    response: str | None = None    # full response text (only if trace.log_prompts = true)
```

### 5.2 Trace File Format

JSONL (one JSON object per line). This enables:
- Streaming writes (no need to hold the whole file in memory)
- Easy `grep`/`jq` filtering
- Append-only (no corruption risk on crash)

Example trace file content:

```jsonl
{"timestamp":"2026-04-09T14:23:01.442Z","stage":"triage","model":"gpt-4o","prompt_hash":"a3f2b7c1d4e5...","response_hash":"b8c3d9e0f1a2...","prompt_tokens":1247,"response_tokens":89,"cost_usd":0.0142,"duration_ms":2341,"cache_hit":false,"prompt":null,"response":null}
{"timestamp":"2026-04-09T14:23:04.891Z","stage":"triage","model":"gpt-4o","prompt_hash":"c5d6e7f8a9b0...","response_hash":"d1e2f3a4b5c6...","prompt_tokens":1103,"response_tokens":67,"cost_usd":0.0124,"duration_ms":1892,"cache_hit":false,"prompt":null,"response":null}
```

### 5.3 Trace Writer

```python
class TraceWriter:
    def __init__(self, config: TraceConfig) -> None: ...
    def open(self) -> None: ...     # open file handle, write header comment
    def write(self, entry: TraceEntry) -> None: ...  # serialize + write + flush
    def close(self) -> None: ...    # close file handle
    def summary(self) -> TraceSummary: ...  # total tokens, total cost, entry count
```

The writer is initialized once at CLI startup and passed through the pipeline via dependency injection (not global state). Each stage receives a reference to the writer.

### 5.4 Security Considerations

- `log_prompts` defaults to `false`. Prompts may contain source code, which could be sensitive.
- When `log_prompts = false`, only hashes are stored. This allows verifying that the same prompt was sent (for reproducibility) without leaking code.
- Trace files should not be committed to version control (added to `.gitignore`).
- Cost tracking enables budget enforcement: if cumulative cost exceeds `models.budget.max_cost_usd`, the pipeline halts with a clear error.

### 5.5 Tasks

| # | Task | Acceptance |
|---|------|------------|
| T1 | Define `TraceEntry` Pydantic model | Model validates correctly, passes mypy |
| T2 | Implement `TraceWriter` class | Writes JSONL, flushes per entry |
| T3 | Wire trace writer into CLI startup/shutdown | Trace file created on `piranesi run`, closed on exit |
| T4 | Implement `log_prompts` toggle | With `log_prompts=false`, prompt/response fields are null |
| T5 | Implement `TraceSummary` | After pipeline, print total tokens + cost |
| T6 | Budget enforcement | Pipeline halts when cost exceeds `max_cost_usd` |

---

## 6. Logging, TUI, and Observability Infrastructure

**Effort estimate: 10-15 ideal hours**

**Design principle: logs are the primary debugging interface.** Piranesi is a CLI tool that orchestrates multiple subsystems (Joern JVM, Docker containers, LLM APIs, Z3 solver). When something goes wrong, the user and the developer need to know exactly what happened, where, and why — without re-running the scan. Every log message must answer: *what operation*, *on what input*, *with what result*, *in how long*.

### 6.1 Structured Logging Design

Use Python's `logging` module with structured key-value output.

- Each pipeline stage gets its own logger: `piranesi.scan`, `piranesi.detect`, `piranesi.triage`, `piranesi.verify`, `piranesi.legal`, `piranesi.patch`, `piranesi.report`.
- Each subsystem gets its own logger: `piranesi.joern`, `piranesi.docker`, `piranesi.llm`, `piranesi.z3`, `piranesi.transpile`.
- Default level: `INFO`. `--verbose` sets `DEBUG`. `--quiet` sets `WARNING`.
- All log messages use structured key-value pairs for machine parseability, even in human-readable mode.

### 6.2 Log Output Modes

**Interactive TTY mode** (default when stdout is a terminal):
- Use `rich` for formatted output with colors, progress bars, and tables.
- Progress indicators for long operations:
  - `[scan]` stage: progress bar over files parsed ("Parsing 847 files... [=====>    ] 423/847")
  - `[detect]` stage: progress bar over source-sink pairs queried
  - `[triage]` stage: spinner per finding being triaged ("Triaging finding f-003 via ensemble...")
  - `[verify]` stage: spinner per finding ("Building sandbox... Starting app... Firing payload... Capturing result...")
  - `[legal]` stage: progress bar over findings being mapped
- Stage transitions clearly delimited:
  ```
  ──── scan ────────────────────────────────────────
  [scan] parsed 847 files in 2.3s (0 parse errors)
  [scan] call graph: 41,209 edges, 3 entry points
  [scan] attack surface: 127 handlers, 34 SQL queries
  [scan] wrote scan.json (2.1 MB)
  
  ──── detect ──────────────────────────────────────
  [detect] querying Joern for data flows...
  [detect] 94 sources × 61 sinks → 23 candidate findings
  [detect] wrote detect.json
  ```
- Errors shown inline with full context:
  ```
  [verify] ERROR building sandbox for f-007
    target: src/api/users.ts:87 (CWE-89)
    reason: npm install failed (exit code 1)
    detail: gyp ERR! build error - node-gyp requires python
    action: marking as UNVERIFIABLE (BUILD_FAILED_IGNORE_SCRIPTS)
  ```

**Non-interactive mode** (piped output, CI, `--json-logs`):
- JSON structured logs, one object per line (JSONL):
  ```json
  {"ts":"2026-04-09T14:23:01.442Z","level":"INFO","logger":"piranesi.scan","event":"parse_complete","files":847,"errors":0,"duration_ms":2300}
  {"ts":"2026-04-09T14:23:03.100Z","level":"INFO","logger":"piranesi.detect","event":"flow_query","sources":94,"sinks":61,"findings":23,"duration_ms":1200}
  {"ts":"2026-04-09T14:23:05.500Z","level":"ERROR","logger":"piranesi.verify","event":"sandbox_build_failed","finding_id":"f-007","reason":"npm_install_failed","exit_code":1,"detail":"gyp ERR! build error"}
  ```
- Every log entry includes: `ts` (ISO 8601), `level`, `logger` (namespace), `event` (machine-readable event name), and event-specific key-value pairs.

### 6.3 Log Levels — What Goes Where

| Level | Use for | Example |
|-------|---------|---------|
| `DEBUG` | Internal state, variable values, decision points | `"Joern query returned 14 flows for source req.body.userId"` |
| `INFO` | Stage progress, summary stats, artifact writes | `"parsed 847 files in 2.3s"`, `"wrote scan.json"` |
| `WARNING` | Degraded operation, recoverable issues, skipped items | `"3 files failed transpilation (skipped)"`, `"Joern server restart (attempt 1/2)"` |
| `ERROR` | Failed operations that produce incomplete results | `"sandbox build failed for f-007"`, `"Z3 solver timeout on f-012"` |
| `CRITICAL` | Unrecoverable failures that halt the pipeline | `"Joern binary not found"`, `"Docker daemon not running"` |

### 6.4 Error Logging Standards

Every error log MUST include:

1. **What failed** — the operation name (`sandbox_build`, `joern_query`, `z3_solve`, `tsc_transpile`)
2. **What it was operating on** — the finding ID, file path, or query being executed
3. **Why it failed** — the error message, exit code, or exception type
4. **What happens next** — the recovery action (`marking as UNVERIFIABLE`, `retrying with fallback model`, `skipping file`)
5. **How to debug** — enough context to reproduce (`tsc command: ...`, `CPGQL query: ...`, `Docker build log: ...`)

Example of a BAD error log (never do this):
```
ERROR: Something went wrong
```

Example of a GOOD error log:
```
[verify] ERROR sandbox exploit failed
  finding: f-003 (CWE-78, command injection)
  target: src/api/admin.ts:142
  payload: "; id"
  sandbox: container piranesi-sandbox-a8f3 (node:20-slim)
  http_status: 500
  response_body: "Internal Server Error" (no command output detected)
  action: marking as LIKELY (response differs from baseline but no confirmed execution)
  debug: reproducer script at piranesi-output/reproducers/f-003.sh
```

### 6.5 Subprocess Logging

Piranesi orchestrates multiple subprocesses (Joern, tsc, Docker, npm). Every subprocess call must log:

- **Before execution**: command being run (with args), working directory
- **After execution**: exit code, duration, stdout/stderr (truncated to 500 chars at INFO, full at DEBUG)
- **On failure**: full stdout/stderr regardless of log level, plus the command that was run (copy-pasteable for manual debugging)

```python
import subprocess
import logging

log = logging.getLogger("piranesi.subprocess")

def run_subprocess(cmd: list[str], cwd: str | None = None, timeout: int = 60) -> subprocess.CompletedProcess:
    log.debug("exec: %s (cwd=%s)", " ".join(cmd), cwd)
    try:
        result = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout)
        log.debug("exit=%d duration=%.1fs stdout=%d chars stderr=%d chars",
                  result.returncode, ..., len(result.stdout), len(result.stderr))
        if result.returncode != 0:
            log.error("subprocess failed: %s\n  exit_code=%d\n  cmd: %s\n  stdout: %s\n  stderr: %s",
                      cmd[0], result.returncode, " ".join(cmd),
                      result.stdout[:2000], result.stderr[:2000])
        return result
    except subprocess.TimeoutExpired:
        log.error("subprocess timeout after %ds: %s", timeout, " ".join(cmd))
        raise
```

### 6.6 TUI Progress Indicators

Use `rich` for all interactive output. Implementation: `src/piranesi/ui.py`.

```python
from rich.console import Console
from rich.progress import Progress, SpinnerColumn, TextColumn, BarColumn, TimeElapsedColumn
from rich.panel import Panel
from rich.table import Table

console = Console(stderr=True)  # progress/status to stderr, structured output to stdout

def stage_header(name: str) -> None:
    console.rule(f"[bold]{name}[/bold]", style="dim")

def file_progress(total: int) -> Progress:
    return Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(),
        TextColumn("{task.completed}/{task.total}"),
        TimeElapsedColumn(),
        console=console,
    )

def finding_spinner(finding_id: str, description: str) -> None:
    # used for per-finding operations (triage, verify)
    ...
```

**Key UX decisions:**
- Progress output goes to **stderr**. Structured JSON output goes to **stdout**. This allows `piranesi scan ./src > scan.json` while still showing progress.
- Progress bars for batch operations (file parsing, flow queries). Spinners for per-item operations (triage, verify).
- On completion, print a summary table:
  ```
  ┌─────────────────────────────────────────┐
  │ Piranesi Scan Summary                   │
  ├──────────────┬──────────────────────────┤
  │ Files parsed │ 847                      │
  │ Findings     │ 23 candidates → 14 triaged → 11 confirmed │
  │ Top CWEs     │ CWE-89 (5), CWE-79 (3), CWE-78 (2), CWE-22 (1) │
  │ Regulatory   │ PDPA s24 (4), MAS TRM 11 (2) │
  │ LLM cost     │ $3.47                    │
  │ Duration     │ 4m 12s                   │
  └──────────────┴──────────────────────────┘
  ```

### 6.7 Debug Mode (`--debug`)

Beyond `--verbose` (which shows DEBUG-level logs), add a `--debug` flag that:
- Dumps full subprocess stdout/stderr for every call (Joern, tsc, Docker, npm)
- Enables `trace.log_prompts = true` (full LLM prompts/responses in trace file)
- Writes Joern CPGQL queries and responses to a debug log file (`piranesi-debug.log`)
- On error, prints the full Python traceback (not just the error message)
- Preserves intermediate artifacts that are normally cleaned up (transpiled JS files, Docker build context)

This flag is for developer debugging, not normal user operation. It produces large output.

### 6.8 Logger Setup

```python
import logging
import sys
from rich.logging import RichHandler

def setup_logging(verbose: bool = False, quiet: bool = False, debug: bool = False, json_logs: bool = False) -> None:
    if debug:
        level = logging.DEBUG
    elif verbose:
        level = logging.DEBUG
    elif quiet:
        level = logging.WARNING
    else:
        level = logging.INFO

    is_tty = sys.stderr.isatty() and not json_logs

    if is_tty:
        handler = RichHandler(
            console=Console(stderr=True),
            rich_tracebacks=True,
            tracebacks_show_locals=debug,
            show_path=debug,
            show_time=verbose or debug,
        )
        fmt = "%(message)s"
    else:
        handler = logging.StreamHandler(sys.stderr)
        fmt = '{"ts":"%(asctime)s","level":"%(levelname)s","logger":"%(name)s","msg":"%(message)s"}'

    logging.basicConfig(level=level, format=fmt, handlers=[handler], force=True)
```

### 6.9 Tasks

| # | Task | Acceptance |
|---|------|------------|
| L1 | Implement `setup_logging()` with all modes (TTY/JSON/debug) | Logging works in TTY, pipe, and debug modes |
| L2 | Create per-stage + per-subsystem loggers | Each stage and subsystem logs under its namespace |
| L3 | Implement `run_subprocess()` wrapper with full logging | All subprocess calls logged with command, exit code, output |
| L4 | Implement `ui.py` with rich progress/spinners/tables | Progress bars for batch ops, spinners for per-item ops |
| L5 | Implement summary table at pipeline completion | Printed to stderr after `piranesi run` |
| L6 | Implement `--debug` flag | Full subprocess output, tracebacks, artifact preservation |
| L7 | Wire `--verbose`, `--quiet`, `--debug`, `--json-logs` into CLI | All flags control logging as specified |
| L8 | Error logging standards enforced | Every ERROR log includes what/on-what/why/next/debug fields |

---

## 7. Testing Infrastructure

**Effort estimate: 4-6 ideal hours**

### 7.1 Directory Structure

```
tests/
├── conftest.py
├── fixtures/
│   ├── typescript/
│   │   ├── simple_xss.ts
│   │   ├── sql_injection.ts
│   │   ├── sanitized_input.ts
│   │   └── express_app/
│   │       ├── app.ts
│   │       ├── routes/
│   │       └── package.json
│   └── configs/
│       ├── default.toml
│       ├── minimal.toml
│       └── invalid.toml
├── test_cli.py
├── test_config.py
├── test_trace.py
├── models/
├── scan/
├── detect/
├── triage/
├── verify/
├── legal/
├── patch/
├── report/
└── llm/
```

### 7.2 conftest.py Fixtures

```python
import pytest
from pathlib import Path
from piranesi.config import PiranesiConfig

@pytest.fixture
def fixtures_dir() -> Path:
    return Path(__file__).parent / "fixtures"

@pytest.fixture
def ts_fixtures(fixtures_dir: Path) -> Path:
    return fixtures_dir / "typescript"

@pytest.fixture
def default_config(tmp_path: Path) -> PiranesiConfig:
    """Returns a PiranesiConfig with defaults, output directed to tmp_path."""
    return PiranesiConfig(output=OutputConfig(output_dir=str(tmp_path / "output")))

@pytest.fixture
def mock_llm(monkeypatch):
    """Patches LiteLLM to return canned responses without making real API calls."""
    # Implementation: monkeypatch litellm.completion to return a mock response
    # Each test can configure the mock's return value
    ...

@pytest.fixture
def config_file(tmp_path: Path):
    """Factory fixture: creates a temporary piranesi.toml with given content."""
    def _create(content: str) -> Path:
        p = tmp_path / "piranesi.toml"
        p.write_text(content)
        return p
    return _create
```

### 7.3 Running Tests

```bash
uv run pytest                    # all tests
uv run pytest -m "not slow"     # skip slow tests
uv run pytest --cov=piranesi    # with coverage
uv run pytest tests/test_cli.py # single module
```

### 7.4 Tasks

| # | Task | Acceptance |
|---|------|------------|
| TE1 | Configure pytest in `pyproject.toml` | `uv run pytest` runs (0 tests collected is OK) |
| TE2 | Write `conftest.py` with all fixtures | Fixtures importable, `mock_llm` patches LiteLLM |
| TE3 | Create fixture directories + placeholder TS files | Fixture files exist and are loadable |
| TE4 | Write tests for config loading | At least 5 tests: defaults, file load, env override, invalid file, missing file |
| TE5 | Write tests for CLI argument parsing | At least 3 tests: `--help` exits 0, missing `--authorized` exits 2, valid invocation |
| TE6 | Write tests for trace writer | At least 3 tests: write entry, JSONL format, `log_prompts` toggle |

---

## 8. CI Setup

**Effort estimate: 4-6 ideal hours**

### 8.1 GitHub Actions Workflow

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main, master]
  pull_request:
    branches: [main, master]

permissions:
  contents: read

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: astral-sh/setup-uv@v4
        with:
          version: "latest"
      - run: uv sync --frozen
      - run: uv run ruff check src/ tests/
      - run: uv run ruff format --check src/ tests/

  typecheck:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: astral-sh/setup-uv@v4
        with:
          version: "latest"
      - run: uv sync --frozen
      - run: uv run mypy src/piranesi/

  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        python-version: ["3.12", "3.13"]
    steps:
      - uses: actions/checkout@v4
      - uses: astral-sh/setup-uv@v4
        with:
          version: "latest"
          python-version: ${{ matrix.python-version }}
      - run: uv sync --frozen
      - run: uv run pytest --cov=piranesi --cov-report=xml
      - uses: codecov/codecov-action@v4
        if: matrix.python-version == '3.12'
        with:
          file: coverage.xml

  build:
    runs-on: ubuntu-latest
    needs: [lint, typecheck, test]
    steps:
      - uses: actions/checkout@v4
      - uses: astral-sh/setup-uv@v4
        with:
          version: "latest"
      - run: uv build
      - uses: actions/upload-artifact@v4
        with:
          name: dist
          path: dist/
```

### 8.2 Tasks

| # | Task | Acceptance |
|---|------|------------|
| CI1 | Write `.github/workflows/ci.yml` | Workflow file is valid YAML, passes `actionlint` |
| CI2 | Verify lint job passes | `ruff check` and `ruff format --check` pass on scaffolding code |
| CI3 | Verify typecheck job passes | `mypy` passes on all stub files |
| CI4 | Verify test job passes | `pytest` runs and exits 0 (even with 0 tests initially) |
| CI5 | Verify build job produces wheel | `uv build` creates a `.whl` in `dist/` |

---

## 9. Milestones and Acceptance Criteria

### Milestone 0.1: Repository Bootstrapped
- **Done when:** `pyproject.toml`, `ruff.toml`, `.gitignore`, all directories and `__init__.py` files exist. `uv sync` installs all deps. `uv build` produces a wheel.
- **Acceptance:** `uv sync && uv build && python -c "from piranesi import __version__; print(__version__)"` prints `0.1.0`.
- **Effort:** 6-8h
- **Dependencies:** None

### Milestone 0.1a: Runtime Dependencies Validated
- **Done when:** Joern is installed and functional. JVM 11+ is present. `tsc` (TypeScript compiler) is available.
- **Acceptance:** `joern --version` prints version. `java -version` prints 11+. `npx tsc --version` prints TS version. `echo 'val x = 1' | joern --script /dev/stdin` completes without error.
- **Effort:** 2-3h (installation + validation, not development)
- **Dependencies:** None (can run in parallel with 0.1)
- **Note:** These are system-level dependencies, not pip packages. Document installation in README and getting-started.md. If any are missing, `piranesi scan` must print a clear error message with installation instructions.

### Milestone 0.2: CLI Skeleton Operational
- **Done when:** `piranesi --help` shows all 8 subcommands. Each subcommand prints "not implemented" and exits 1. `--authorized` gate works. `--yes` flag works.
- **Acceptance:** `uv run piranesi --help` exits 0. `uv run piranesi scan . 2>&1` exits 2 (no `--authorized`). `uv run piranesi scan . --authorized --yes 2>&1` exits 1 (not implemented).
- **Effort:** 8-10h
- **Dependencies:** Milestone 0.1

### Milestone 0.3: Configuration System Complete
- **Done when:** `piranesi.toml` loads correctly. Pydantic validation catches bad config. Env var overrides work.
- **Acceptance:** Create a `piranesi.toml` with custom values. Run `piranesi scan . --authorized --yes --verbose`. Verbose output shows loaded config values matching the file. Set `PIRANESI_MODELS_SCANNER=test-model` and verify override in verbose output.
- **Effort:** 6-8h
- **Dependencies:** Milestone 0.1

### Milestone 0.4: Trace Logging Infrastructure
- **Done when:** Trace file is created on CLI startup. `TraceWriter` can write entries. Budget enforcement halts on exceeded cost.
- **Acceptance:** Run `piranesi scan . --authorized --yes`. Verify `.piranesi-trace.jsonl` is created (empty is OK, no LLM calls yet). Unit tests for `TraceWriter` pass.
- **Effort:** 6-8h
- **Dependencies:** Milestone 0.3

### Milestone 0.4a: Logging, TUI, and Observability
- **Done when:** `setup_logging()` supports all 4 modes (TTY/JSON/debug/quiet). `run_subprocess()` logs all subprocess calls. `ui.py` provides progress bars, spinners, and summary tables. Error logs include full context (what/on-what/why/next/debug). `--debug` flag preserves artifacts and dumps full output.
- **Acceptance:** `piranesi scan . --authorized --yes` shows rich progress bar. `piranesi scan . --authorized --yes --json-logs 2>log.jsonl` produces valid JSONL. `piranesi scan . --authorized --yes --debug` shows full tracebacks and subprocess output. An intentional error (e.g., missing Joern) produces a log message with all 5 required context fields.
- **Effort:** 8-12h
- **Dependencies:** Milestone 0.3

### Milestone 0.5: Testing Infrastructure and CI
- **Done when:** `uv run pytest` runs with all fixtures available. CI workflow passes on push. Lint, typecheck, test, build all green.
- **Acceptance:** `uv run pytest` exits 0 with config/CLI/trace tests passing. Push to GitHub triggers CI. All 4 jobs pass.
- **Effort:** 8-12h
- **Dependencies:** Milestone 0.4

### Summary Table

| Milestone | Description | Effort (h) | Depends On | Cumulative (h) |
|-----------|-------------|------------|------------|-----------------|
| 0.1 | Repository bootstrapped | 6-8 | -- | 6-8 |
| 0.2 | CLI skeleton operational | 8-10 | 0.1 | 14-18 |
| 0.3 | Configuration system complete | 6-8 | 0.1 | 20-26 |
| 0.4 | Trace logging infrastructure | 6-8 | 0.3 | 26-34 |
| 0.4a | Logging, TUI, and observability | 8-12 | 0.3 | 34-46 |
| 0.5 | Testing infrastructure and CI | 8-12 | 0.4, 0.4a | 42-58 |

**Total phase estimate: 42-58 ideal hours** (upper bound accounts for integration overhead between milestones).

---

## 10. Phase Dependencies

```
Phase 0 (this phase)
  ├── Blocked by: Nothing
  ├── Blocks: Phase 1 (Taint Analysis)
  ├── Blocks: Phase 2 (Exploit Verification)
  ├── Blocks: Phase 3 (Regulatory Engine)
  ├── Blocks: Phase 4 (LLM Orchestration)
  ├── Blocks: Phase 5 (Evaluation Harness)
  ├── Blocks: Phase 6 (Report Generation)
  ├── Can start: Immediately
  └── Parallel opportunities: Milestones 0.2 and 0.3 can run in parallel
      (both depend only on 0.1). 0.4 depends on 0.3. 0.5 depends on 0.4.
```

Critical path through this phase: 0.1 → 0.3 → 0.4 → 0.5 (28-39h). Milestone 0.2 (CLI skeleton) can be done in parallel with 0.3 by a second contributor, saving 6-8h of wall-clock time.
