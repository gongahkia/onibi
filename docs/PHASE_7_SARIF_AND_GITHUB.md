# Phase 7: SARIF Output + CI Integration

**Estimated effort: 20-25 ideal hours**
**Blocked by: Phase 6 (pipeline working)**
**Blocks: Nothing (adoption enabler)**
**Target milestone: v0.2.0**

---

## 1. Phase Overview

Piranesi produces JSON and Markdown reports. Neither format integrates with existing security tooling ecosystems. This phase adds SARIF (Static Analysis Results Interchange Format) output, a pre-built Docker runtime image, and provider-agnostic CI integration documentation.

SARIF is the lingua franca of static analysis tools. VS Code SARIF Viewer, DefectDojo, SonarQube, GitHub Code Scanning, GitLab SAST, and dozens of other platforms consume SARIF natively. Without SARIF, Piranesi's findings exist in a silo.

Piranesi remains a local CLI tool. Users clone their target repository, run `piranesi run ./path`, and consume standard output formats. There is no provider-specific integration built into the tool itself — CI wiring is the user's responsibility, aided by documentation and examples.

---

## 2. SARIF Report Generator

**Estimated effort: 10-12h**

### 2.1 SARIF 2.1.0 Schema

Implement `src/piranesi/report/sarif.py`:

- `generate_sarif(report: PiranesiReport) -> dict` — converts a `PiranesiReport` to a SARIF 2.1.0 JSON object.
- Map Piranesi concepts to SARIF:

| Piranesi | SARIF |
|----------|-------|
| `ConfirmedFinding` | `result` |
| `vuln_class` (CWE-89) | `result.ruleId` + `rule.id` |
| `severity` | `result.level` (error/warning/note) |
| `taint_path` | `result.codeFlows[].threadFlows[].locations[]` |
| `source` location | `result.locations[0]` |
| `sink` location | `result.relatedLocations[]` |
| `exploit_payload` | `result.message.text` (summary) |
| `patch_diff` | `result.fixes[].artifactChanges[]` |
| `LegalAssessment` | `result.properties.regulatory` (property bag) |
| Piranesi version | `run.tool.driver.version` |

### 2.2 SARIF Code Flows

The taint path is the key differentiator. Each `TaintStep` maps to a `threadFlowLocation`:

```json
{
  "codeFlows": [{
    "threadFlows": [{
      "locations": [
        {"location": {"physicalLocation": {"artifactLocation": {"uri": "src/routes/users.ts"}, "region": {"startLine": 15}}, "message": {"text": "req.body.username (tainted source)"}}}
      ]
    }]
  }]
}
```

### 2.3 SARIF Rules

Each CWE becomes a SARIF `reportingDescriptor` in `tool.driver.rules[]`:

```json
{
  "id": "CWE-89",
  "name": "SQLInjection",
  "shortDescription": {"text": "SQL Injection"},
  "fullDescription": {"text": "User-controlled input reaches a SQL query without parameterization."},
  "helpUri": "https://cwe.mitre.org/data/definitions/89.html",
  "properties": {"tags": ["security", "sql-injection", "owasp-a03"]}
}
```

### 2.4 CLI Integration

- Add `--format sarif` option to `piranesi run` and `piranesi report`.
- When `--format sarif`, write `{output_dir}/report.sarif.json` instead of / in addition to `report.json`.
- Validate output against SARIF 2.1.0 schema in tests.

### 2.5 Tests

`tests/test_report/test_sarif.py`:
- Test SARIF output validates against the JSON schema (use `jsonschema` library)
- Test code flow mapping preserves taint path ordering
- Test all severity levels map correctly
- Test regulatory properties appear in property bags
- Test `--format sarif` CLI flag produces valid file

---

## 3. Docker Runtime Image

**Estimated effort: 4-5h**

### 3.1 Dockerfile

Create `Dockerfile` at project root — production runtime image with Joern + JVM + Piranesi pre-installed:

```dockerfile
FROM eclipse-temurin:17-jre-jammy
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip python3-venv curl unzip nodejs npm docker.io \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://github.com/joernio/joern/releases/latest/download/joern-install.sh | bash
COPY . /opt/piranesi
RUN pip install --no-cache-dir /opt/piranesi
ENTRYPOINT ["piranesi"]
```

This image eliminates environment setup. Users run:
```bash
docker run --rm -v $(pwd):/workspace ghcr.io/gongahkia/piranesi:latest run /workspace --authorized --yes
```

### 3.2 Image Publishing

- Publish to `ghcr.io/gongahkia/piranesi:latest` and versioned tags (`0.2.0`).
- CI builds and pushes the image on tagged releases.

---

## 4. Provider-Agnostic CI Documentation

**Estimated effort: 5-6h**

### 4.1 `docs/ci-integration.md`

Write a guide with copy-pasteable CI configuration snippets for common providers. Piranesi does not ship provider-specific integrations — it is a CLI tool that produces standard output formats (JSON, Markdown, SARIF). Users wire it into their CI pipeline themselves.

The guide includes:

**GitHub Actions example:**
```yaml
- name: Run Piranesi
  run: |
    pip install piranesi
    piranesi run . --format sarif --authorized --yes --output .piranesi-output
- name: Upload SARIF
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: .piranesi-output/report.sarif.json
```

**GitLab CI example:**
```yaml
piranesi-scan:
  image: ghcr.io/gongahkia/piranesi:latest
  script:
    - piranesi run . --format sarif --authorized --yes --output piranesi-output
  artifacts:
    reports:
      sast: piranesi-output/report.sarif.json
```

**Generic CI (any provider):**
```bash
piranesi run ./target --format sarif --authorized --yes --output ./results
# results/report.sarif.json — import into any SARIF-compatible tool
# results/report.json — machine-readable JSON
# results/report.md — human-readable Markdown
```

**Docker-based CI (no local install):**
```bash
docker run --rm -v $(pwd):/workspace ghcr.io/gongahkia/piranesi:latest \
  run /workspace --format sarif --authorized --yes --output /workspace/results
```

### 4.2 Fail-on-Findings Pattern

Document the standard pattern for failing a CI pipeline when findings are detected:

```bash
piranesi run . --format sarif --authorized --yes --output results
# check exit code: 0 = clean, 1 = findings detected
if [ $? -eq 1 ]; then echo "Security findings detected"; exit 1; fi
```

---

## 5. Acceptance Criteria

- [ ] `piranesi run --format sarif` produces valid SARIF 2.1.0
- [ ] SARIF includes code flows (taint paths) and fix suggestions (patches)
- [ ] Regulatory assessments included in SARIF property bags
- [ ] Docker runtime image builds and runs successfully
- [ ] `docs/ci-integration.md` with examples for GitHub Actions, GitLab CI, and generic CI
- [ ] No provider-specific code in Piranesi core
