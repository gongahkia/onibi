# Phase 15: Finding Suppression + Baseline Comparison

**Estimated effort: 25-35 ideal hours**
**Blocked by: Phase 6 (pipeline working)**
**Blocks: Nothing (independent feature)**
**Target milestone: v0.3.0**

---

## 1. Phase Overview

Production SAST tools need two features Piranesi currently lacks: the ability to suppress known false positives so they don't re-appear, and the ability to diff scans to show only new/fixed findings. Without these, CI integration is noisy (same FPs every run) and developers can't track progress.

---

## 2. Finding Suppression System

**Estimated effort: 12-15h**

### 2.1 `.piranesi-ignore` File

Project-level suppression file at the repo root:

```yaml
# .piranesi-ignore
suppressions:
  - id: "a1b2c3d4..."           # finding fingerprint
    reason: "Parameterized in production ORM layer"
    author: "dev@example.com"
    date: "2026-04-09"
    expires: "2027-04-09"        # optional expiry

  - cwe: CWE-918                # suppress all SSRF in this path
    path: "src/internal-api/**"
    reason: "Internal service mesh, not user-facing"

  - cwe: CWE-79
    path: "src/admin/**"
    reason: "Admin panel behind auth, accepted risk"
    ticket: "SEC-1234"           # optional ticket reference
```

### 2.2 Inline Comment Suppression

Support suppression comments in source code:

```typescript
// piranesi:suppress CWE-79 reason:"admin-only endpoint"
res.send(userInput);
```

Parser in `src/piranesi/detect/suppression.py`:
- Scan source files for `piranesi:suppress` comments
- Extract CWE ID, optional reason, optional ticket
- Match against findings by file + line proximity (±2 lines)

### 2.3 Suppression Application

In the detect stage, after finding extraction:
1. Load `.piranesi-ignore` + inline suppressions
2. Match each finding against suppression rules
3. Suppressed findings are still recorded but marked `suppressed: true`
4. Report shows suppressed count separately: "6 findings (2 suppressed)"

### 2.4 CLI Commands

```bash
# add a suppression interactively
piranesi suppress <finding-id> --reason "accepted risk" --ticket SEC-123

# list all suppressions
piranesi suppress --list

# remove an expired suppression
piranesi suppress --prune
```

### 2.5 Stable Finding Fingerprints

Current fingerprint is SHA-256 of `vuln_class|source_location|sink_location`. This is unstable when code moves. Improve:

```python
def stable_fingerprint(finding: CandidateFinding) -> str:
    """Hash: vuln_class + source_function + sink_function + taint_path_shape."""
    material = f"{finding.vuln_class}|{source_function_name}|{sink_function_name}|{path_length}"
    return hashlib.sha256(material.encode()).hexdigest()[:16]
```

Use function names + path topology instead of line numbers. Stable across line-number shifts from formatting/refactoring.

---

## 3. Baseline Comparison

**Estimated effort: 10-12h**

### 3.1 `piranesi diff` Command

```bash
piranesi diff ./results-baseline ./results-current
```

Output:
```
Piranesi Diff: baseline (2026-04-01) → current (2026-04-09)

NEW (2):
  + CWE-89 SQLi in src/routes/users.ts:45 → db.query()
  + CWE-79 XSS in src/routes/admin.ts:23 → res.send()

FIXED (1):
  - CWE-78 CMDi in src/utils/exec.ts:12 → exec()  [was in baseline, now gone]

UNCHANGED (4):
  = CWE-22 Path Traversal in src/routes/files.ts:67
  = CWE-918 SSRF in src/services/fetch.ts:34
  ...

Summary: 2 new, 1 fixed, 4 unchanged
```

### 3.2 Finding Matching

Match findings across scans by stable fingerprint. If fingerprint exists in both → unchanged. Only in new → new finding. Only in baseline → fixed.

### 3.3 `piranesi baseline` Command

```bash
# save current scan as baseline
piranesi baseline save --from ./results --to .piranesi-baseline.json

# auto-compare against baseline on next run
piranesi run ./target --baseline .piranesi-baseline.json
```

### 3.4 CI Integration Pattern

```bash
# first run: save baseline
piranesi run . --output results --authorized --yes
piranesi baseline save --from results --to .piranesi-baseline.json

# PR check: compare against baseline, fail only on NEW findings
piranesi run . --output results-pr --baseline .piranesi-baseline.json --authorized --yes
piranesi diff .piranesi-baseline.json results-pr --fail-on-new
```

---

## 4. Acceptance Criteria

- [ ] `.piranesi-ignore` file loaded and applied to findings
- [ ] Inline `piranesi:suppress` comments parsed
- [ ] `piranesi suppress` CLI command adds/lists/prunes suppressions
- [ ] Stable fingerprints survive line-number shifts
- [ ] `piranesi diff` shows new/fixed/unchanged findings
- [ ] `piranesi baseline save` creates baseline artifact
- [ ] `--baseline` flag on `piranesi run` auto-diffs
- [ ] `--fail-on-new` exits 1 only for new findings (not unchanged)
- [ ] Suppressed findings shown in report with suppression reason
