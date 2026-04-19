# Track D Wave 1: Sparse Slice Expansion

## Scope
Wave 1 focuses on five high-priority sparse `language+framework` slices and their corresponding `cwe+framework` slices:

1. `javascript|koa` (`CWE-79|koa`, `CWE-601|koa`)
2. `go|gorilla` (`CWE-352|gorilla`)
3. `javascript|feathers` (`CWE-943|feathers`)
4. `java|ratpack` (`CWE-338|ratpack`)
5. `php|symfony` (`CWE-89|symfony`)

This gives a 10-slice first wave (5 language/framework + 5 CWE/framework).

## Target Policy
Using `--min-count 8`, each slice should trend toward:

- `target_true_positive=4`
- `target_false_positive=4`

## Before/After Snapshot

1. `javascript|koa`: `count 4 (tp=4, fp=0)` -> `count 6 (tp=4, fp=2)`
2. `go|gorilla`: `count 2 (tp=1, fp=1)` -> `count 4 (tp=2, fp=2)`
3. `javascript|feathers`: `count 2 (tp=1, fp=1)` -> `count 4 (tp=2, fp=2)`
4. `java|ratpack`: `count 2 (tp=1, fp=1)` -> `count 4 (tp=2, fp=2)`
5. `php|symfony`: `count 2 (tp=1, fp=1)` -> `count 4 (tp=2, fp=2)`

## Added Replayable Fixtures

1. `eval/cve_fixtures/cwe352-synthetic-gorilla-origin-check-vuln.go`
2. `eval/cve_fixtures/cwe352-synthetic-gorilla-origin-check-safe.go`
3. `eval/cve_fixtures/cwe943-synthetic-feathers-filter-merge-vuln.js`
4. `eval/cve_fixtures/cwe943-synthetic-feathers-filter-merge-safe.js`
5. `eval/cve_fixtures/cwe338-synthetic-ratpack-session-token-vuln.java`
6. `eval/cve_fixtures/cwe338-synthetic-ratpack-session-token-safe.java`
7. `eval/cve_fixtures/cwe89-synthetic-symfony-doctrine-concat-vuln.php`
8. `eval/cve_fixtures/cwe89-synthetic-symfony-doctrine-concat-safe.php`
9. `eval/cve_fixtures/cwe79-CVE-2025-32379-koa-redirect-body-vuln.js`
10. `eval/cve_fixtures/cwe79-CVE-2025-32379-koa-redirect-body-safe.js`
11. `eval/cve_fixtures/cwe601-CVE-2025-8129-koa-back-redirect-vuln.js`
12. `eval/cve_fixtures/cwe601-CVE-2025-8129-koa-back-redirect-safe.js`
13. `eval/cve_fixtures/cwe601-CVE-2025-62595-koa-back-redirect-bypass-vuln.js`
14. `eval/cve_fixtures/cwe601-CVE-2021-23384-koa-remove-trailing-slashes-vuln.js`

## Ground Truth Updates

1. Converted legacy non-replayable Koa entries to replayable fixture paths:
   - `gt-082`, `gt-083`, `gt-084`, `gt-085`
2. Added first-wave sparse-slice balancing entries:
   - `gt-508` to `gt-517`
3. Added `taint_field_path` and `field_sensitive_label` metadata to updated Koa entries and all new entries.

## Remaining Gap To Target
Wave 2 should continue these same slices until each reaches `count >= 8` and a stable TP/FP split near `4/4`.
