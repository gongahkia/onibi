# Phase 37: Unified Vulnerability Database & Advisory Sync

**Estimated effort: 50-65 ideal hours**
**Blocked by: Phase 16 (OWASP coverage), Phase 28 (crypto/transport — shared CWE enrichment)**
**Blocks: Phase 38 (threat modeling — EPSS/exploit data feeds DREAD scoring)**
**Target milestone: v0.7.0**

---

## 1. Overview

### 1.1 Current State

`src/piranesi/detect/dependencies.py` shells out to `npm audit` and `pip-audit` for vulnerability data. Findings are parsed from JSON output and normalized to `CandidateFinding` objects with CWE-1395 classification.

### 1.2 Limitations

| Gap | Impact |
|-----|--------|
| npm + PyPI only | No Go, Java, Rust, Ruby coverage |
| No EPSS scoring | Cannot prioritize by real-world exploitation probability |
| No exploit availability data | No visibility into weaponized or PoC-available CVEs |
| Online-only | Air-gapped environments get zero dependency findings |
| Tool dependency | Requires `npm` and `pip-audit` binaries installed |
| No cross-source dedup | Same CVE from multiple tools creates duplicates |

### 1.3 Goal

Unified local advisory database that aggregates multiple upstream sources, enriches with EPSS and exploit availability, and replaces tool-specific shelling with direct lockfile-to-advisory lookups.

---

## 2. Advisory Sources

### 2.1 Source Matrix

| Source | API | Auth | Coverage | Refresh Cadence |
|--------|-----|------|----------|-----------------|
| NVD | REST v2 (`services.nvd.nist.gov/rest/json/cves/2.0`) | API key for >5 req/30s | All ecosystems via CPE | Modified-since queries |
| GHSA | GraphQL (`api.github.com/graphql`) | `GITHUB_TOKEN` | npm, PyPI, Go, Maven, RubyGems, Rust | Cursor-based pagination |
| OSV | REST (`api.osv.dev/v1/query`) | None | npm, PyPI, Go, crates.io, Maven, NuGet | Per-ecosystem batch queries |
| Go vuln DB | REST (`vuln.go.dev/ID/`) | None | Go modules | Index file for incremental |
| Sonatype OSS Index | REST (`ossindex.sonatype.org/api/v3/`) | Free API key | Maven, npm, PyPI | Batch coordinate queries |
| Snyk | REST (`snyk.io/api/v1/`) | Optional public token | npm, PyPI, Go, Maven, Ruby | Per-package queries |

### 2.2 Normalized Advisory Model

```python
# src/piranesi/advisory/models.py
@dataclass(frozen=True)
class Advisory:
    advisory_id: str                    # canonical ID (CVE preferred)
    cve_id: str | None                  # CVE-YYYY-NNNNN
    ghsa_id: str | None                 # GHSA-xxxx-xxxx-xxxx
    cwe_ids: tuple[str, ...]            # CWE-89, CWE-79, etc.
    title: str
    description: str
    affected_packages: tuple[AffectedPackage, ...]
    severity: str                       # critical/high/medium/low
    cvss_score: float | None            # 0.0-10.0
    cvss_vector: str | None             # CVSS:3.1/AV:N/AC:L/...
    epss_score: float | None            # 0.0-1.0
    epss_percentile: float | None       # 0.0-1.0
    exploit_status: ExploitStatus       # none/poc_available/weaponized/in_the_wild
    fix_available: bool
    published_date: str                 # ISO 8601
    modified_date: str                  # ISO 8601
    sources: tuple[str, ...]            # which upstream sources reported this
    references: tuple[str, ...]         # URLs

@dataclass(frozen=True)
class AffectedPackage:
    ecosystem: str                      # npm/pypi/go/maven/crates/rubygems
    name: str                           # package name
    vulnerable_ranges: tuple[str, ...]  # version constraint strings
    fixed_versions: tuple[str, ...]     # first safe versions

class ExploitStatus(str, Enum):
    NONE = "none"
    POC_AVAILABLE = "poc_available"
    WEAPONIZED = "weaponized"
    IN_THE_WILD = "in_the_wild"
```

### 2.3 Source Priority & Dedup

When the same CVE appears in multiple sources:
1. Prefer NVD for CVSS score (authoritative).
2. Prefer GHSA for affected version ranges (most accurate for open-source).
3. Prefer OSV for ecosystem-specific package names.
4. Merge `cwe_ids`, `references`, `fixed_versions` from all sources.
5. Use latest `modified_date` across sources.

---

## 3. Local Database

### 3.1 Storage

SQLite at `.piranesi-cache/advisory.db`. Created on first `piranesi advisory sync` or auto-created when DB is missing.

### 3.2 Schema

```sql
CREATE TABLE IF NOT EXISTS advisories (
    advisory_id   TEXT PRIMARY KEY,
    cve_id        TEXT,
    ghsa_id       TEXT,
    cwe_ids       TEXT,           -- JSON array: ["CWE-89","CWE-79"]
    title         TEXT NOT NULL,
    description   TEXT,
    severity      TEXT NOT NULL,  -- critical/high/medium/low
    cvss_score    REAL,
    cvss_vector   TEXT,
    epss_score    REAL,
    epss_percentile REAL,
    exploit_status TEXT NOT NULL DEFAULT 'none',
    published_date TEXT,
    modified_date  TEXT,
    sources       TEXT,           -- JSON array: ["nvd","ghsa","osv"]
    references    TEXT,           -- JSON array of URLs
    fetched_at    TEXT NOT NULL   -- ISO 8601, when we last synced this record
);

CREATE TABLE IF NOT EXISTS affected_packages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    advisory_id     TEXT NOT NULL REFERENCES advisories(advisory_id),
    ecosystem       TEXT NOT NULL,
    name            TEXT NOT NULL,
    vulnerable_ranges TEXT,       -- JSON array: [">=1.0.0 <1.2.3"]
    fixed_versions  TEXT,         -- JSON array: ["1.2.3"]
    fix_available   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sync_metadata (
    source         TEXT PRIMARY KEY,
    last_sync      TEXT NOT NULL,  -- ISO 8601 timestamp
    last_cursor    TEXT,           -- pagination cursor / modified-since token
    record_count   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_affected_pkg ON affected_packages(ecosystem, name);
CREATE INDEX idx_advisory_cve ON advisories(cve_id);
CREATE INDEX idx_advisory_ghsa ON advisories(ghsa_id);
CREATE INDEX idx_advisory_severity ON advisories(severity);
CREATE INDEX idx_advisory_epss ON advisories(epss_score);
```

### 3.3 Module Layout

```
src/piranesi/advisory/
    __init__.py
    models.py           # Advisory, AffectedPackage, ExploitStatus
    db.py               # SQLite read/write, schema migration
    sync.py             # orchestrate multi-source sync
    sources/
        __init__.py
        nvd.py          # NVD REST v2 client
        ghsa.py         # GitHub Security Advisories GraphQL client
        osv.py          # OSV REST client
        go_vuln.py      # vuln.go.dev client
        ossindex.py     # Sonatype OSS Index client
    epss.py             # FIRST EPSS API client
    exploit.py          # exploit availability checker
    lookup.py           # query advisory DB by package+version
    version_match.py    # semver, PEP 440, Go module version matching
```

### 3.4 Sync Logic

```python
# src/piranesi/advisory/sync.py
def sync_advisories(
    db: AdvisoryDB,
    *,
    sources: tuple[str, ...] = ("osv", "ghsa", "nvd"),
    full: bool = False,
    ecosystems: tuple[str, ...] | None = None,
) -> SyncResult:
    """Incremental sync from upstream sources.
    
    If full=True, ignore last_cursor and fetch everything.
    If ecosystems specified, only sync advisories affecting those ecosystems.
    """
```

Incremental sync:
1. Read `sync_metadata` for each source to get `last_cursor` / `last_sync`.
2. Fetch only modified/new advisories since that timestamp.
3. Normalize to `Advisory` model.
4. Upsert into `advisories` + `affected_packages` tables.
5. Update `sync_metadata` with new cursor/timestamp.
6. After all sources synced, run EPSS enrichment pass (Section 4).
7. After EPSS, run exploit availability check (Section 5).

### 3.5 Auto-Sync

During `piranesi run`, before dependency scanning:
1. Check if `advisory.db` exists. If not, prompt user to run `piranesi advisory sync`.
2. If DB exists but `sync_metadata.last_sync` is >24h old, log a warning: "advisory database is stale, run `piranesi advisory sync`".
3. `--advisory-auto-sync` flag: auto-sync if DB >24h stale (opt-in, not default — avoids surprise network calls).

---

## 4. EPSS Integration

### 4.1 API

FIRST EPSS API: `https://api.first.org/data/v1/epss`
- Free, no API key required.
- Supports batch queries: `?cve=CVE-2024-1234,CVE-2024-5678`.
- Returns `epss` (probability 0.0-1.0) and `percentile` (0.0-1.0).
- Rate limit: 100 requests/minute.

### 4.2 Enrichment Flow

```python
# src/piranesi/advisory/epss.py
def enrich_epss(db: AdvisoryDB, *, batch_size: int = 100) -> int:
    """Fetch EPSS scores for all advisories with a CVE ID.
    
    Returns count of advisories updated.
    Batches CVE IDs into groups of batch_size to stay within API limits.
    """
```

1. Query `advisories` where `cve_id IS NOT NULL AND (epss_score IS NULL OR fetched_at < now - 7d)`.
2. Batch CVE IDs into groups of 100.
3. For each batch, `GET https://api.first.org/data/v1/epss?cve=CVE-...,CVE-...`.
4. Update `epss_score` and `epss_percentile` columns.
5. Respect rate limit: 100ms delay between batches.

### 4.3 Risk Labels

| EPSS Score | Percentile | Label | Report Display |
|------------|------------|-------|----------------|
| >= 0.5 | >= 97th | `actively_exploited_risk` | EPSS: 0.72 (97th pctl) -- ACTIVELY EXPLOITED RISK |
| >= 0.1 | >= 90th | `high_exploit_probability` | EPSS: 0.15 (92nd pctl) -- HIGH EXPLOIT PROBABILITY |
| >= 0.01 | >= 50th | `moderate_exploit_probability` | EPSS: 0.03 (65th pctl) |
| < 0.01 | < 50th | `low_exploit_probability` | EPSS: 0.002 (12th pctl) |

### 4.4 Impact on Severity

EPSS does not override CVSS-based severity, but is displayed alongside it and used for prioritization in reports:
- Findings sorted by: `(severity_rank, -epss_score, -cvss_score)`.
- Executive summary calls out any findings with EPSS >= 0.1.

---

## 5. Exploit Availability Tracking

### 5.1 Sources

| Source | Method | Data |
|--------|--------|------|
| Metasploit modules | GitHub API: `rapid7/metasploit-framework`, search `modules/exploits/` for CVE references | `weaponized` |
| Exploit-DB | GHDB/CSV mirror at `gitlab.com/exploit-database/exploitdb` | `poc_available` |
| PoC-in-GitHub | GitHub topic search: `topic:cve-YYYY-NNNNN` OR `topic:poc` | `poc_available` |
| CISA KEV | `https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json` | `in_the_wild` |

### 5.2 Exploit Status Resolution

```python
# src/piranesi/advisory/exploit.py
def check_exploit_availability(
    db: AdvisoryDB,
    *,
    cve_ids: Sequence[str] | None = None,
) -> int:
    """Check exploit availability for advisories.
    
    Priority: in_the_wild > weaponized > poc_available > none.
    CISA KEV is authoritative for in_the_wild.
    Returns count of advisories updated.
    """
```

Resolution order (highest wins):
1. CVE in CISA KEV catalog → `in_the_wild`.
2. CVE referenced in Metasploit module → `weaponized`.
3. CVE has Exploit-DB entry → `poc_available`.
4. CVE has GitHub PoC repo → `poc_available`.
5. None of the above → `none`.

### 5.3 Impact on Finding Severity

| Original Severity | Exploit Status | Adjusted Severity |
|-------------------|----------------|-------------------|
| medium | `in_the_wild` | high |
| medium | `weaponized` | high |
| high | `in_the_wild` | critical |
| low | `poc_available` | medium |
| * | `none` | unchanged |

Severity bump is additive metadata (`adjusted_severity` field), original `severity` preserved.

---

## 6. Dependency Scanning Enhancement

### 6.1 Lockfile Parsers

Replace tool-shelling with direct lockfile parsing for advisory DB lookup:

| Lockfile | Ecosystem | Parser |
|----------|-----------|--------|
| `package-lock.json` | npm | Already parsed in `dependencies.py` — reuse `_load_npm_package_versions` |
| `yarn.lock` | npm | New parser: regex-based name@version extraction |
| `pnpm-lock.yaml` | npm | New parser: YAML `packages:` section |
| `Pipfile.lock` | pypi | JSON `default` + `develop` sections |
| `requirements.txt` / `*.txt` | pypi | Regex: `name==version` extraction |
| `go.sum` | go | Line format: `module v0.1.2 h1:hash=` |
| `pom.xml` | maven | XML: `<dependency><groupId>:<artifactId>:<version>` |
| `build.gradle` / `build.gradle.kts` | maven | Regex: `implementation 'group:name:version'` |
| `Cargo.lock` | crates | TOML: `[[package]]` sections |
| `Gemfile.lock` | rubygems | `SPECS:` section parsing |

```python
# src/piranesi/advisory/lookup.py
def lookup_dependencies(
    db: AdvisoryDB,
    project_root: Path,
) -> list[CandidateFinding]:
    """Parse all lockfiles, query advisory DB, return findings.
    
    Falls back to npm audit / pip-audit if advisory DB is empty.
    """
```

### 6.2 Version Range Matching

```python
# src/piranesi/advisory/version_match.py
def is_vulnerable(
    package_version: str,
    vulnerable_ranges: Sequence[str],
    ecosystem: str,
) -> bool:
    """Check if package_version falls within any vulnerable range.
    
    Dispatches to ecosystem-specific comparator:
    - npm: semver (node-semver compatible)
    - pypi: PEP 440 (packaging.version)
    - go: Go module pseudo-version comparison
    - maven: Maven version ordering
    - crates: Cargo semver
    """
```

Dependencies:
- `packaging` (already in deps) for PEP 440.
- `semver` or inline semver parser for npm ranges.
- Custom comparators for Go/Maven.

### 6.3 Migration Path

1. **Phase 1**: advisory DB exists alongside existing `npm audit`/`pip-audit` flow.
2. **Phase 2**: when advisory DB is populated, use it as primary; fall back to tool-shelling.
3. **Phase 3**: tool-shelling becomes optional (for users who prefer it or for ecosystems not yet parsed).

Existing `parse_npm_audit_payload` and `parse_pip_audit_payload` in `detect/dependencies.py` remain functional — no breaking changes.

---

## 7. Offline Mode

### 7.1 Export

```
piranesi advisory export advisory.db
piranesi advisory export advisory.db --ecosystems npm,pypi
piranesi advisory export advisory.db --since 2026-01-01
```

Produces a standalone SQLite file with all tables and indexes. Filtered by ecosystem or date if specified.

### 7.2 Import

```
piranesi advisory import advisory.db
piranesi advisory import advisory.db --merge   # merge with existing, don't replace
```

Copies/merges into `.piranesi-cache/advisory.db`.

### 7.3 Pre-Built DB Distribution

For organizations:
- CI job syncs advisory DB nightly.
- Export and publish as artifact.
- Developer machines `piranesi advisory import` from shared artifact.
- Air-gapped environments receive DB via sneakernet/artifact transfer.

---

## 8. CLI Commands

```
piranesi advisory sync                         # incremental sync from all sources
piranesi advisory sync --full                  # full re-sync (ignore cursors)
piranesi advisory sync --sources osv,ghsa      # sync specific sources only
piranesi advisory sync --ecosystems npm,pypi   # sync specific ecosystems only
piranesi advisory status                       # show DB stats, last sync per source
piranesi advisory search <query>               # search advisories by CVE/package/keyword
piranesi advisory export <path>                # export DB to file
piranesi advisory import <path>                # import DB from file
piranesi advisory import <path> --merge        # merge into existing DB
piranesi advisory epss                         # refresh EPSS scores
piranesi advisory exploits                     # refresh exploit availability data
```

### 8.1 Status Output

```
Advisory Database Status (.piranesi-cache/advisory.db)
  Total advisories:    148,392
  Affected packages:   312,847
  With EPSS scores:    142,109 (95.8%)
  With exploit data:   148,392 (100%)

  Source         Last Sync            Records   Cursor
  ─────────────────────────────────────────────────────
  osv            2026-04-11 08:30     89,412    2026-04-11T08:30:00Z
  ghsa           2026-04-11 08:31     62,334    cursor_abc123
  nvd            2026-04-11 08:35     43,291    2026-04-11T08:35:00Z

  EPSS refresh:  2026-04-11 08:36
  Exploit check: 2026-04-11 08:37
  DB size:       42.3 MB
```

---

## 9. Configuration

In `piranesi.toml`:

```toml
[advisory]
auto_sync = false                        # auto-sync if DB > 24h stale
sync_interval_hours = 24                 # staleness threshold
sources = ["osv", "ghsa", "nvd"]         # enabled sources
ecosystems = []                          # empty = all ecosystems

[advisory.nvd]
api_key = ""                             # optional, env: PIRANESI_NVD_API_KEY

[advisory.ghsa]
token = ""                               # optional, env: GITHUB_TOKEN

[advisory.epss]
enabled = true
threshold_high = 0.1                     # label as high_exploit_probability
threshold_critical = 0.5                 # label as actively_exploited_risk
refresh_days = 7                         # re-fetch EPSS if older than N days

[advisory.exploit]
enabled = true
check_cisa_kev = true
check_metasploit = true
check_exploitdb = false                  # slower, disabled by default
check_github_poc = false                 # slower, disabled by default

[advisory.severity_bump]
enabled = true                           # auto-bump severity based on exploit status
```

Environment variable overrides: `PIRANESI_NVD_API_KEY`, `GITHUB_TOKEN`, `PIRANESI_ADVISORY_AUTO_SYNC=1`.

---

## 10. Report Integration

### 10.1 Finding Metadata Enrichment

Each `CandidateFinding` from dependency scanning gets additional metadata fields:

```python
metadata = {
    # existing fields...
    "epss_score": 0.15,
    "epss_percentile": 0.92,
    "epss_label": "high_exploit_probability",
    "exploit_status": "weaponized",
    "exploit_sources": ["metasploit"],
    "adjusted_severity": "critical",     # if bumped
    "advisory_sources": ["nvd", "ghsa", "osv"],
    "cvss_vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
    "fix_available": True,
    "fix_version": "2.1.4",
}
```

### 10.2 Executive Summary Additions

```
Dependency Findings: 12 (3 critical, 5 high, 4 medium)
  With known exploits:     4 (2 in-the-wild, 1 weaponized, 1 PoC)
  EPSS > 0.1:             6 (high exploitation probability)
  Fix available:           9 (75%)
  No fix available:        3 (monitor + mitigate)
```

### 10.3 Markdown Report Section

For each dependency finding, add:
- EPSS score + percentile + label.
- Exploit status + sources.
- CVSS vector string.
- Fix availability + recommended version.
- Links to advisory URLs.

---

## 11. Tests

### 11.1 Advisory Source Mocks

```python
# tests/test_advisory/test_osv.py
def test_osv_parse_response():
    """Mock OSV API response, verify Advisory model populated correctly."""

# tests/test_advisory/test_ghsa.py
def test_ghsa_parse_graphql():
    """Mock GHSA GraphQL response, verify normalization."""

# tests/test_advisory/test_nvd.py
def test_nvd_parse_cve_item():
    """Mock NVD REST v2 response, verify CVSS extraction."""
```

### 11.2 Version Range Matching

```python
# tests/test_advisory/test_version_match.py
@pytest.mark.parametrize("version,range_str,expected", [
    ("1.2.3", ">=1.0.0 <1.3.0", True),
    ("1.3.0", ">=1.0.0 <1.3.0", False),
    ("2.0.0-alpha.1", ">=2.0.0-alpha.0 <2.0.0", True),
    ("0.0.0", ">=1.0.0", False),
])
def test_npm_semver_matching(version, range_str, expected):
    assert is_vulnerable(version, [range_str], "npm") == expected

@pytest.mark.parametrize("version,range_str,expected", [
    ("1.2.3", ">=1.0,<1.3", True),
    ("1.3.0", ">=1.0,<1.3", False),
    ("2.0.0a1", ">=2.0.0a0,<2.0.0", True),
])
def test_pep440_matching(version, range_str, expected):
    assert is_vulnerable(version, [range_str], "pypi") == expected
```

### 11.3 EPSS Enrichment

```python
# tests/test_advisory/test_epss.py
def test_epss_enrichment(mock_epss_api):
    """Verify EPSS scores written to DB after enrichment."""

def test_epss_batch_splitting():
    """Verify >100 CVEs split into batches correctly."""

def test_epss_label_thresholds():
    """Verify label assignment at boundary values."""
```

### 11.4 Exploit Availability

```python
# tests/test_advisory/test_exploit.py
def test_cisa_kev_marks_in_the_wild(mock_kev_json):
    """CVE in CISA KEV → exploit_status = in_the_wild."""

def test_metasploit_marks_weaponized(mock_msf_search):
    """CVE with Metasploit module → exploit_status = weaponized."""

def test_exploit_priority_resolution():
    """in_the_wild > weaponized > poc_available > none."""
```

### 11.5 Offline Mode

```python
# tests/test_advisory/test_offline.py
def test_export_import_roundtrip(tmp_path):
    """Export DB, import into fresh location, verify data integrity."""

def test_merge_import(tmp_path):
    """Import with --merge adds new records without deleting existing."""
```

### 11.6 DB Lookup Integration

```python
# tests/test_advisory/test_lookup.py
def test_lookup_npm_package(populated_db):
    """Query DB for known-vulnerable npm package, verify finding generated."""

def test_lookup_no_match(populated_db):
    """Query DB for safe package version, verify no finding."""

def test_lockfile_parsing_yarn():
    """Parse yarn.lock fixture, verify extracted packages."""

def test_lockfile_parsing_go_sum():
    """Parse go.sum fixture, verify extracted modules."""
```

---

## 12. Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| NVD API rate limits | High | Slow initial sync | API key config, incremental sync, OSV as primary |
| Advisory data staleness | Medium | Missed new CVEs | Auto-sync warnings, configurable interval |
| Version range parsing edge cases | Medium | FP/FN in matching | Comprehensive parametrized tests, ecosystem-specific parsers |
| DB size growth (>100MB) | Low | Disk usage on CI | Ecosystem filtering, periodic pruning of old advisories |
| Upstream API changes | Medium | Broken sync | Per-source error handling, graceful degradation, source-specific tests |
| EPSS API availability | Low | Missing enrichment | Cache scores for 7 days, advisory usable without EPSS |
