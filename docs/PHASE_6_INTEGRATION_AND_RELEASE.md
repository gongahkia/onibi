# Phase 6: Integration and Release

**Estimated effort: 60-80 ideal hours**
**Blocked by: Phases 0-5 (all)**
**Blocks: Nothing (final phase)**

---

## 1. Phase Overview

By this point, every individual stage of Piranesi works in isolation: taint analysis produces findings, Z3 generates exploit payloads, the sandbox verifies them, the regulatory engine maps obligations, LLM orchestration triages and patches, and the eval harness measures quality. None of them talk to each other through a single entry point.

Phase 6 builds the `piranesi run` orchestrator that connects all stages into a seamless pipeline, produces combined reports merging technical/legal/patch output, writes documentation sufficient for a user to install and run the tool without reading source code, and prepares the project for public release on PyPI and GitHub.

This phase is largely integration glue, report templating, and documentation. The hard engineering is done. The risk here is polish and edge cases, not algorithmic complexity.

---

## 2. Pipeline Orchestrator -- `piranesi run`

**Estimated effort: 12-15h**

### 2.1 Pipeline Stages

The full pipeline executes sequentially:

```
scan -> detect -> triage -> verify -> legal -> patch -> report
```

Each stage is a callable with a uniform interface:

```python
from dataclasses import dataclass
from pathlib import Path
from typing import Any

@dataclass
class StageResult:
    stage: str # stage name
    success: bool
    artifact: Any # stage-specific output model (pydantic)
    elapsed_s: float
    error: str | None = None

StageFunc = Callable[[PiranesiConfig, StageResult | None], StageResult]
```

### 2.2 Orchestrator Implementation Sketch

```python
import time
import json
from pathlib import Path
from piranesi.config import PiranesiConfig
from piranesi.stages import STAGE_REGISTRY # ordered dict of stage name -> StageFunc

STAGE_ORDER = ["scan", "detect", "triage", "verify", "legal", "patch", "report"]

def run_pipeline(
    config: PiranesiConfig,
    resume: bool = False,
    dry_run: bool = False,
    output_dir: Path = Path(".piranesi-out"),
) -> list[StageResult]:
    output_dir.mkdir(parents=True, exist_ok=True)
    results: list[StageResult] = []
    prev_result: StageResult | None = None

    for stage_name in STAGE_ORDER:
        artifact_path = output_dir / f"{stage_name}.json"

        # resume: skip stages with existing artifacts
        if resume and artifact_path.exists():
            prev_result = StageResult(
                stage=stage_name,
                success=True,
                artifact=json.loads(artifact_path.read_text()),
                elapsed_s=0.0,
            )
            results.append(prev_result)
            _log(f"[resume] skipping {stage_name} (artifact exists)")
            continue

        if dry_run:
            _log(f"[dry-run] would execute: {stage_name}")
            continue

        stage_func = STAGE_REGISTRY[stage_name]
        _log(f"[{stage_name}] starting...")
        t0 = time.monotonic()

        try:
            result = stage_func(config, prev_result)
            result.elapsed_s = time.monotonic() - t0
            # write intermediate artifact
            artifact_path.write_text(
                json.dumps(result.artifact, default=str, indent=2)
            )
            _log(f"[{stage_name}] done ({result.elapsed_s:.1f}s)")
        except Exception as e:
            result = StageResult(
                stage=stage_name,
                success=False,
                artifact=None,
                elapsed_s=time.monotonic() - t0,
                error=str(e),
            )
            _log(f"[{stage_name}] FAILED: {e}")
            # save partial results
            _save_partial(output_dir, results, result)
            results.append(result)
            break # halt pipeline on failure

        results.append(result)
        prev_result = result

    return results

def _save_partial(output_dir: Path, completed: list[StageResult], failed: StageResult):
    summary = {
        "completed": [r.stage for r in completed],
        "failed": failed.stage,
        "error": failed.error,
    }
    (output_dir / "_partial.json").write_text(json.dumps(summary, indent=2))

def _log(msg: str):
    # simple print for v1; swap for rich.progress if TTY detected
    print(msg)
```

### 2.3 CLI Integration

```python
import typer
from pathlib import Path

app = typer.Typer()

@app.command()
def run(
    target: Path = typer.Argument(..., help="path to TS/JS project root"),
    config: Path = typer.Option("piranesi.toml", help="config file path"),
    output_dir: Path = typer.Option(".piranesi-out", help="output directory"),
    resume: bool = typer.Option(False, help="resume from prior run artifacts"),
    dry_run: bool = typer.Option(False, help="show what would run without executing"),
):
    cfg = load_config(config, target)
    results = run_pipeline(cfg, resume=resume, dry_run=dry_run, output_dir=output_dir)
    _print_summary(results)
```

### 2.4 Key Design Decisions

- **Sequential, not parallel.** Each stage depends on the prior stage's output. No parallelism at the stage level. (Within stages, e.g. triage, parallelism is handled internally.)
- **JSON artifacts.** Every intermediate result is written to disk as JSON. This enables debugging, resume, and external tooling.
- **Fail-fast.** If any stage fails, the pipeline halts. Partial results are saved. The user can fix the issue and `--resume`.
- **No daemon mode.** `piranesi run` is a one-shot command. Watch-mode / CI integration is out of scope for v1.
- **Progress reporting.** v1 uses simple print statements. If `sys.stdout.isatty()`, use `rich.progress.Progress` for a progress bar. Non-TTY (CI) gets plain text.

---

## 3. Combined Report Generator

**Estimated effort: 12-15h**

### 3.1 Report Data Model

Each confirmed finding in the final report combines data from multiple stages:

```python
from pydantic import BaseModel

class CombinedFinding(BaseModel):
    finding_id: str # unique identifier
    # from detect
    cwe: str
    title: str
    severity: str # critical / high / medium / low
    confidence: float # 0.0-1.0
    taint_path: list[TaintPathNode]
    source_location: SourceLocation
    sink_location: SourceLocation
    # from verify
    verified: bool
    exploit_payload: str | None
    reproducer_script: str | None
    verification_method: str # "smt+sandbox" | "smt-only" | "unverifiable"
    # from legal
    regulatory_obligations: list[RegulatoryObligation]
    # from patch
    patch_diff: str | None
    pr_body: str | None

class PiranesiReport(BaseModel):
    scan_metadata: ScanMetadata
    executive_summary: ExecutiveSummary
    findings: list[CombinedFinding]
    appendix: ReportAppendix
```

### 3.2 Output Formats

#### JSON (`--format json`)

Machine-readable. Full `PiranesiReport` serialized via `model_dump_json(indent=2)`. Intended for CI integration, downstream tooling, and SARIF conversion (future).

#### Markdown (`--format markdown`)

Human-readable report for stakeholders. Rendered via Jinja2 templates.

Template location: `piranesi/templates/report.md.j2`

#### PR Body (`--format pr`)

GitHub-flavored markdown for each finding, suitable for direct use as a pull request body. Includes the patch diff, a summary of the vulnerability, and the regulatory impact.

### 3.3 Report Structure (Markdown)

```markdown
# Piranesi Security Analysis Report

**Target:** /path/to/project
**Date:** 2026-04-09
**Version:** piranesi v0.1.0

## Executive Summary

- **Files scanned:** 347
- **Findings detected:** 12
- **Findings confirmed (verified exploit):** 5
- **Severity breakdown:** 1 critical, 2 high, 2 medium
- **Top regulatory concern:** PDPA S24 — mandatory breach notification
  required within 3 days for 2 confirmed findings.

## Finding 1: SQL Injection in User Login

**CWE:** CWE-89
**Severity:** Critical
**Confidence:** 0.94
**Verified:** Yes (SMT + sandbox)

### Taint Path

```
src/routes/auth.ts:23  req.body.username  (SOURCE: http_param)
  -> src/routes/auth.ts:25  sanitize(username)  (PASSTHROUGH)
  -> src/db/queries.ts:47  buildQuery(sanitized)  (PASSTHROUGH)
  -> src/db/queries.ts:52  db.query(queryStr)  (SINK: sql_query)
```

### Exploit

**Payload:** `' OR 1=1; --`
**Verification:** Sandbox execution confirmed HTTP 200 with
unauthorized data in response body.

### Regulatory Obligations

| Framework | Section | Obligation | Deadline | Penalty |
|-----------|---------|------------|----------|---------|
| PDPA | S24 | Notify PDPC of breach | 3 days | Up to SGD 1M |
| MAS TRM | 11.2.3 | Log and report incident | 30 days | Supervisory action |

### Patch

```diff
--- a/src/db/queries.ts
+++ b/src/db/queries.ts
@@ -50,3 +50,3 @@
-  const queryStr = `SELECT * FROM users WHERE name = '${name}'`;
+  const queryStr = `SELECT * FROM users WHERE name = $1`;
-  return db.query(queryStr);
+  return db.query(queryStr, [name]);
```

---

## Appendix

- **Scan duration:** 4m 23s
- **Model routing:** Claude Sonnet (triage), GPT-4o (patch gen)
- **Total LLM cost:** $0.47
- **Taint analysis time:** 2m 11s
- **Z3 queries:** 8 (5 sat, 2 unsat, 1 timeout)
```

### 3.4 Template System

Jinja2 templates stored in `piranesi/templates/`:

```
piranesi/templates/
  report.md.j2          # full markdown report
  finding.md.j2         # per-finding section (included by report.md.j2)
  pr_body.md.j2         # PR body for a single finding
  executive_summary.j2  # summary block
```

Rendering:

```python
from jinja2 import Environment, PackageLoader

def render_report(report: PiranesiReport, format: str) -> str:
    env = Environment(loader=PackageLoader("piranesi", "templates"))
    template = env.get_template(f"report.{format}.j2")
    return template.render(report=report)
```

### 3.5 SARIF Export (Future)

Not in v1 scope. SARIF (Static Analysis Results Interchange Format) would enable integration with GitHub Code Scanning, VS Code, and other tooling. Planned for v1.1.

---

## 4. Example Runs

**Estimated effort: 10-12h**

### 4.1 Target Applications

#### a) NodeGoat (OWASP)

- **Repo:** https://github.com/OWASP/NodeGoat
- **Why:** Purpose-built vulnerable Node.js application. Contains SQL injection, XSS, SSRF, insecure deserialization, and more. Well-documented known vulnerabilities.
- **Setup:**

```bash
git clone https://github.com/OWASP/NodeGoat.git
cd NodeGoat
piranesi run . --config examples/nodegoat.toml --output-dir nodegoat-results/
```

- **Expected findings:** SQL injection (CWE-89), XSS (CWE-79), path traversal (CWE-22), insecure session management.
- **Expected misses:** vulnerabilities requiring runtime-only analysis (e.g., race conditions), vulnerabilities in dependencies (not in scope -- Piranesi analyzes first-party code only).

#### b) OWASP Juice Shop (TypeScript portions)

- **Repo:** https://github.com/juice-shop/juice-shop
- **Why:** Full TypeScript application. Widely used for security training. Contains ~100 known challenges/vulnerabilities.
- **Caveat:** Juice Shop is large and complex. Piranesi v1 may not handle its full module graph. Focus analysis on `routes/` and `lib/` directories.

```bash
git clone https://github.com/juice-shop/juice-shop.git
cd juice-shop
piranesi run . --include "routes/**,lib/**" --config examples/juiceshop.toml
```

- **Expected findings:** SQL injection in search, XSS in product reviews, path traversal in file serving.
- **Discussion:** document false positive rate honestly. If Piranesi flags 20 things and only 8 are real, report that.

#### c) Hand-Crafted Vulnerable Express App

A minimal Express.js application with 3-5 planted vulnerabilities. Checked into the repo under `examples/vuln-express/`.

Planted vulnerabilities:
1. **SQL injection** -- unsanitized user input in raw SQL query (CWE-89)
2. **Reflected XSS** -- user input rendered in HTML response without escaping (CWE-79)
3. **Path traversal** -- user-controlled path joined with base directory, no sanitization (CWE-22)
4. **Command injection** -- user input passed to `child_process.exec()` (CWE-78)
5. **SSRF** -- user-controlled URL passed to `fetch()` / `axios.get()` (CWE-918)

```bash
cd examples/vuln-express
piranesi run . --output-dir results/
```

This app serves as the primary integration test. All 5 vulnerabilities should be detected and verified. If any are missed, that's a bug, not a limitation.

### 4.2 Example Run Documentation Format

For each example run, document:

1. **Setup steps** -- exact commands to clone, configure, and run
2. **CLI invocation** -- the full `piranesi run` command with all flags
3. **Representative output** -- terminal output showing progress and summary
4. **Results discussion:**
   - True positives (correctly identified vulnerabilities)
   - False positives (flagged but not real)
   - False negatives (missed known vulnerabilities)
   - Verification success rate (how many detected findings were verified via sandbox)
   - Regulatory mapping accuracy (spot-check 2-3 legal memos for correctness)
5. **Timing** -- total scan time, broken down by stage

### 4.3 Integration Test Derivation

The example runs double as integration tests:

```python
# tests/integration/test_pipeline.py
import subprocess
import json
from pathlib import Path

def test_vuln_express_full_pipeline():
    result = subprocess.run(
        ["piranesi", "run", "examples/vuln-express/", "--format", "json",
         "--output-dir", "/tmp/piranesi-test-out"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0
    report = json.loads((Path("/tmp/piranesi-test-out") / "report.json").read_text())
    assert len(report["findings"]) >= 3 # at least 3 of 5 planted vulns
    confirmed = [f for f in report["findings"] if f["verified"]]
    assert len(confirmed) >= 2 # at least 2 verified
```

---

## 5. Documentation

**Estimated effort: 8-10h**

### 5.1 Documentation Files

| File | Purpose | Length |
|------|---------|--------|
| `README.md` | Project overview, quick start, badges | ~200 lines |
| `docs/getting-started.md` | Installation, first scan, understanding output | ~150 lines |
| `docs/configuration.md` | Full config reference (piranesi.toml) | ~200 lines |
| `docs/adding-rules.md` | How to add custom regulatory rules | ~100 lines |
| `docs/adding-sources-sinks.md` | How to add custom taint source/sink specs | ~100 lines |

### 5.2 README.md Structure

```markdown
# Piranesi

CLI-native BYOK cybersecurity analysis for TypeScript/JavaScript.

## What It Does

Scans TS/JS source code for security vulnerabilities using inter-procedural
taint analysis, generates verified exploits via SMT constraint solving +
sandboxed execution, and maps confirmed findings to regulatory obligations.

## Quick Start

pip install piranesi  # or: uv pip install piranesi
piranesi run /path/to/your/project

## Requirements

- Python 3.12+
- Docker (for exploit verification sandbox)
- At least one LLM API key (see Configuration)

## Configuration

Create a `piranesi.toml` in your project root. See docs/configuration.md.

## Output

[screenshot or example output]

## License

Apache 2.0
```

### 5.3 CLI Help

Every subcommand must have comprehensive `--help` output. Typer generates this from docstrings and parameter annotations.

```
$ piranesi --help
Usage: piranesi [OPTIONS] COMMAND [ARGS]...

  Piranesi: Security analysis for TypeScript/JavaScript.

Commands:
  run       Run the full analysis pipeline.
  scan      Parse and build the project IR.
  detect    Run taint analysis to find vulnerabilities.
  triage    LLM-assisted triage and severity assessment.
  verify    Generate and verify exploits via SMT + sandbox.
  legal     Map findings to regulatory obligations.
  patch     Generate patches for confirmed vulnerabilities.
  report    Generate combined report from pipeline artifacts.
  version   Show version information.
```

### 5.4 No Documentation Site for v1

No mkdocs, Sphinx, or Read the Docs. Markdown files in `docs/` are sufficient. A documentation site adds maintenance burden disproportionate to the user base at v1.

---

## 6. Release Checklist

**Estimated effort: 5-8h**

```
- [ ] Runtime dependencies validated: Joern (joern --version), JVM 11+ (java -version), tsc (npx tsc --version), Docker (docker --version)
- [ ] All eval harness tests pass with precision >= configured threshold
- [ ] CI green: ruff lint, mypy strict, pytest, uv build
- [ ] pyproject.toml metadata complete:
      - name = "piranesi"
      - version = "0.1.0"
      - description, author, license, URLs, classifiers
      - Python >= 3.12 requirement
      - All dependencies pinned with compatible ranges
- [ ] README.md updated with real output examples (not mock data)
- [ ] CHANGELOG.md written for v0.1.0
- [ ] `uv build` produces clean sdist and wheel
- [ ] `pip install dist/piranesi-0.1.0-py3-none-any.whl` works in clean venv
- [ ] `piranesi --version` prints "piranesi 0.1.0"
- [ ] `piranesi run --help` shows all options with descriptions
- [ ] Example runs documented and reproducible on a clean machine
- [ ] SECURITY.md present (see section 8)
- [ ] LICENSE file present (Apache 2.0)
- [ ] No API keys, secrets, or credentials in codebase:
      grep -r "sk-" --include="*.py" .
      grep -r "AKIA" --include="*.py" .
      grep -r "password" --include="*.toml" .
- [ ] Docker dependency documented (required for verify stage)
- [ ] .gitignore covers: __pycache__, .piranesi-out/, *.egg-info/, dist/
- [ ] GitHub repo settings: branch protection on main, require PR reviews
```

---

## 7. License Choice

**Estimated effort: 2h**

**Recommendation: Apache 2.0.**

### Justification

| Criterion | Apache 2.0 | MIT | GPL v3 |
|-----------|-----------|-----|--------|
| Commercial use | Yes | Yes | Yes (with copyleft) |
| Patent grant | Explicit | None | Implicit |
| Copyleft | No | No | Yes |
| Dependency compatibility | All deps are MIT | All deps are MIT | All deps are MIT |
| Adoption friction | Low | Lowest | High for commercial |

**Why Apache 2.0 over MIT:** The explicit patent grant matters. Piranesi implements SMT-backed exploit generation and a novel taint analysis pipeline. While no patents are being filed, the patent grant protects downstream users from future patent claims by contributors. This is a real concern for security tooling adopted by enterprises.

**Why not GPL v3:** Copyleft would require anyone distributing a modified version to release their source code. This limits commercial adoption (e.g., a security vendor building a SaaS wrapper around Piranesi). For a tool that needs adoption to validate its approach, GPL is counterproductive. The goal is usage, not control.

**Why not MIT:** MIT is simpler and more permissive, but lacks the patent grant. For a project with this much novel technique, the patent grant is worth the extra 2 paragraphs of license text.

**Dependency compatibility check:**
- tree-sitter: MIT
- tree-sitter-typescript: MIT
- z3-solver: MIT
- LiteLLM: MIT (BSD-3-Clause for some components)
- typer: MIT
- pydantic: MIT
- Jinja2: BSD-3-Clause
- docker-py: Apache 2.0

All compatible with Apache 2.0. No issues.

---

## 8. Security Disclosure Policy

**Estimated effort: 3-5h**

### SECURITY.md

```markdown
# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Piranesi itself, please report
it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

### How to Report

Option 1 (preferred): Use GitHub Security Advisories.
  Go to: https://github.com/<org>/piranesi/security/advisories/new

Option 2: Email security@<domain>.

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Impact assessment (what can an attacker do?)
- Suggested fix (if you have one)

### Response Timeline

- **Acknowledge:** Within 48 hours of report.
- **Triage:** Within 7 days. We will confirm whether we consider this a
  valid security issue and its severity.
- **Fix:** Depends on severity. Critical: target 14 days. High: target
  30 days. Medium/Low: next release.
- **Disclosure:** We ask reporters to allow 90 days from initial report
  before public disclosure.

### Scope

The following are in scope:
- Vulnerabilities in Piranesi's own code (e.g., if the report parser
  can be exploited via a malicious scan target)
- Sandbox escapes (if a malicious target project can break out of the
  Docker sandbox during verification)
- Information leaks (if Piranesi exposes API keys or sensitive data
  in its output)

The following are out of scope:
- Vulnerabilities in target projects that Piranesi scans (that's what
  Piranesi is for)
- Vulnerabilities in dependencies (report those upstream)

### Bug Bounty

No bug bounty program for v1. This is a solo open-source project.

### Acknowledgment

We will credit reporters in the CHANGELOG and release notes (unless
they prefer to remain anonymous).
```

### Self-Referential Security Considerations

Piranesi is a security tool that processes untrusted input (arbitrary source code). Attack vectors against Piranesi itself:

1. **Malicious source code that exploits tree-sitter parser bugs.** Mitigation: tree-sitter is written in C and is well-fuzzed, but parser bugs are possible. Monitor tree-sitter CVEs.
2. **Sandbox escape via crafted Dockerfile or package.json.** Mitigation: the sandbox runs with `--network=none`, limited resources, and no volume mounts to sensitive host paths. Review Docker security settings carefully.
3. **Prompt injection via source code comments.** If source code contains LLM prompt injection in comments, the triage/patch LLM calls could be manipulated. Mitigation: sanitize code snippets sent to LLMs, or use structured output formats that constrain LLM responses.
4. **Denial of service via pathological input.** A target project with deeply nested callbacks or circular dependencies could cause the taint analyzer to hang. Mitigation: analysis timeouts per file and per function.

---

## 9. Versioning and Release Strategy

**Estimated effort: 2-3h**

### Versioning Scheme

- **Semver:** `MAJOR.MINOR.PATCH`
- **Initial release:** `v0.1.0`
- **0.x convention:** pre-stable. Breaking changes expected between minor versions (0.1 -> 0.2 may break config format, CLI flags, report schema, etc.)
- **1.0.0 criteria:** stable taint analysis, verified exploit generation working on real-world targets, regulatory engine covering at least 2 frameworks, eval harness demonstrating measurable precision/recall.

### Release Process

1. All CI checks pass (lint, typecheck, test, build)
2. Update `__version__` in `piranesi/__init__.py`
3. Update `CHANGELOG.md`
4. Tag: `git tag v0.1.0`
5. Build: `uv build`
6. Publish: `uv publish` (or `twine upload dist/*`)
7. Create GitHub Release with changelog body and wheel artifact attached

### Distribution

- **PyPI:** primary distribution channel. `pip install piranesi` or `uv pip install piranesi`.
- **GitHub Releases:** wheel artifacts attached to release tags.
- **No binary distribution.** No PyInstaller, no Nuitka, no standalone executables. Python-only for v1. Users need Python 3.12+ and Docker.
- **No Docker image for Piranesi itself.** Piranesi uses Docker to sandbox target apps, but Piranesi itself runs on the host. A containerized Piranesi would need Docker-in-Docker, which adds complexity for no clear benefit in v1.
- **No Homebrew formula.** Premature for v1 user base.

### Post-Release

- Monitor GitHub Issues for installation problems (especially Z3 on macOS ARM).
- Track PyPI download stats for adoption signal.
- Plan v0.2.0 based on user feedback and eval harness gaps.

---

## 10. Milestones with Effort Estimates

| Milestone | Effort (hours) | Dependencies |
|-----------|---------------|--------------|
| Pipeline orchestrator (`piranesi run`) | 12-15 | All stage implementations |
| Combined report generator | 12-15 | Report data models from all stages |
| Jinja2 templates (markdown, PR body) | 4-5 | Report data model finalized |
| Example run: hand-crafted vuln app | 3-4 | Full pipeline working |
| Example run: NodeGoat | 4-5 | Full pipeline working |
| Example run: Juice Shop | 3-4 | Full pipeline working |
| Documentation (README, getting-started, config ref) | 8-10 | Stable CLI interface |
| Release checklist execution | 5-8 | Everything above |
| SECURITY.md + LICENSE | 3-5 | None (can do early) |
| Versioning setup + PyPI prep | 2-3 | pyproject.toml finalized |
| **Total** | **60-80** | |

### Suggested Execution Order

1. SECURITY.md + LICENSE (no dependencies, do first)
2. Jinja2 templates (can draft with mock data)
3. Pipeline orchestrator
4. Combined report generator
5. Hand-crafted vuln app example run (first integration test)
6. NodeGoat + Juice Shop example runs
7. Documentation
8. Release checklist

---

## 11. Phase Dependencies

```
Phase 0 (Foundations) ----+
Phase 1 (Taint Analysis) -+
Phase 2 (Exploit Verify) -+---> Phase 6 (Integration & Release)
Phase 3 (Regulatory) -----+
Phase 4 (LLM Orchestr.) --+
Phase 5 (Eval Harness) ---+
```

- **Blocked by:** All phases 0-5. The orchestrator cannot be fully tested until every stage is implemented.
- **Blocks:** Nothing. This is the terminal phase.
- **Early-start opportunities:**
  - SECURITY.md + LICENSE can be written immediately (Phase 0 or earlier).
  - Jinja2 report templates can be drafted with mock data during Phase 2-3.
  - Documentation structure can be outlined during any phase.
  - The hand-crafted vulnerable Express app can be built during Phase 1 (it's also useful as a test target for taint analysis development).
  - The orchestrator skeleton (stage interface, CLI wiring) can be built during Phase 0, with stub implementations for each stage.
