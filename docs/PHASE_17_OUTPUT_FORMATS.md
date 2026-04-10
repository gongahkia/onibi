# Phase 17: Additional Output Formats + CLI Commands

**Estimated effort: 20-25 ideal hours**
**Blocked by: Phase 7 (SARIF done)**
**Blocks: Nothing (independent feature)**
**Target milestone: v0.3.0**

---

## 1. Phase Overview

Piranesi outputs JSON, Markdown, and SARIF. CI pipelines and security teams need additional formats: JUnit XML (standard CI test results), CSV (bulk analysis), and configurable exit codes. This phase also adds the `piranesi init` command for project scaffolding.

---

## 2. JUnit XML Output

**Estimated effort: 6-8h**

### 2.1 Mapping

```xml
<testsuite name="piranesi" tests="6" failures="4" errors="0" skipped="2">
  <testcase classname="CWE-89" name="SQLi in src/routes/users.ts:45" time="0.5">
    <failure message="SQL Injection: req.body.userId reaches db.query() without parameterization"
            type="CWE-89">
      Taint path: req.body.userId → processUser() → db.query()
      Severity: HIGH
      Exploit: ' OR 1=1; --
    </failure>
  </testcase>
  <testcase classname="CWE-79" name="XSS suppressed in src/admin/view.ts:12">
    <skipped message="Suppressed: admin-only endpoint (SEC-1234)"/>
  </testcase>
</testsuite>
```

- Each confirmed finding → `<testcase>` with `<failure>`
- Suppressed findings → `<testcase>` with `<skipped>`
- `--format junit` flag on `piranesi run` and `piranesi report`

### 2.2 CI Integration

JUnit XML is natively consumed by Jenkins, GitLab CI, GitHub Actions (via `dorny/test-reporter`), and CircleCI. Document usage in `docs/ci-integration.md`.

---

## 3. CSV Output

**Estimated effort: 4-5h**

### 3.1 Schema

```csv
id,cwe_id,cwe_name,severity,source_file,source_line,sink_file,sink_line,taint_source,taint_sink,exploit_payload,regulatory_frameworks,suppressed,suppression_reason
a1b2c3d4,CWE-89,SQL Injection,HIGH,src/routes/users.ts,45,src/db/queries.ts,12,req.body.userId,db.query(),"' OR 1=1; --",PDPA|CCPA,false,
```

- `--format csv` flag
- Write to `{output_dir}/findings.csv`
- Include all fields needed for spreadsheet analysis

---

## 4. Configurable Exit Codes

**Estimated effort: 3-4h**

### 4.1 Severity Threshold

```bash
# fail only on HIGH or CRITICAL findings
piranesi run . --fail-severity high --authorized --yes

# fail on any finding (current default)
piranesi run . --fail-severity low --authorized --yes

# never fail (always exit 0)
piranesi run . --no-fail --authorized --yes
```

### 4.2 Exit Code Mapping

| Exit Code | Meaning |
|-----------|---------|
| 0 | No findings (or `--no-fail`) |
| 1 | Findings detected above threshold |
| 2 | Configuration error |
| 3 | Runtime error (Joern unavailable, Docker failed) |
| 4 | Budget exceeded |

---

## 5. `piranesi init` Command

**Estimated effort: 5-6h**

```bash
# scaffold piranesi.toml for detected framework
piranesi init
# Detected: Express + TypeScript
# Created: piranesi.toml with Express defaults
# Created: .piranesi-ignore (empty template)

# specify framework explicitly
piranesi init --framework nestjs
```

Implementation:
1. Detect framework via `scan/framework.py`
2. Generate `piranesi.toml` from a template with framework-appropriate defaults
3. Generate empty `.piranesi-ignore` with comment explaining format
4. Print next steps ("Run `piranesi run .` to scan")

---

## 6. Acceptance Criteria

- [ ] `--format junit` produces valid JUnit XML
- [ ] `--format csv` produces importable CSV
- [ ] `--fail-severity` configures exit code threshold
- [ ] `--no-fail` always exits 0
- [ ] Exit codes 0-4 documented
- [ ] `piranesi init` scaffolds config for detected framework
- [ ] Updated `docs/ci-integration.md` with JUnit XML examples
