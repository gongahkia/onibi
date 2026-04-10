# Phase 29: Ground Truth Expansion via CVE Mining

**Estimated effort: 60-80 ideal hours**
**Blocked by: Phase 9 (ground truth foundation), Phase 16 (OWASP coverage)**
**Blocks: Calibration tuning, precision/recall benchmarking at scale**
**Target milestone: v0.3.0**

---

## 1. Overview

### 1.1 Current State

Piranesi has 186 ground truth entries across 12 CWE classes:

| Label | Count |
|-------|-------|
| True Positive | 144 |
| False Positive | 39 |
| Other (edge/info) | 3 |
| **Total** | **186** |

Per-CWE distribution (TP + FP combined):

| CWE | TP | FP | Total | Notes |
|-----|----|----|-------|-------|
| CWE-89 SQLi | 50 | 14 | 64 | strongest coverage |
| CWE-79 XSS | 48 | 9 | 57 | strongest coverage |
| CWE-22 Path Traversal | 21 | 3 | 24 | |
| CWE-918 SSRF | 17 | 6 | 23 | |
| CWE-78 CmdInj | 13 | 4 | 17 | |
| CWE-601 Open Redirect | 12 | 1 | 13 | |
| CWE-502 Deserialization | 7 | 1 | 8 | |
| CWE-94 Code Injection | 6 | 0 | 6 | no FP coverage |
| CWE-434 File Upload | 6 | 1 | 7 | |
| CWE-77 Command Injection (variant) | 1 | 0 | 1 | severely underrepresented |
| CWE-611 XXE | 1 | 0 | 1 | severely underrepresented |
| CWE-444 HTTP Smuggling | 1 | 0 | 1 | severely underrepresented |

Complexity breakdown: 125 simple, 40 interprocedural, 18 context-sensitive.

### 1.2 Goal

Expand to **400+ entries** across **20+ CWE classes** by mining real-world CVEs. Priorities:

1. Deepen existing CWEs with real CVE-backed entries (not just synthetic patterns)
2. Add 8+ new CWE classes relevant to web application security
3. Increase interprocedural and cross-module entries (currently only 40/186 = 21.5%)
4. Add CVE provenance metadata to every mined entry

### 1.3 Sources

| Source | API / Access | Rate Limit | Coverage |
|--------|-------------|------------|----------|
| NVD (NIST) | REST API v2.0 (`services.nvd.nist.gov/rest/json/cves/2.0`) | 50 req/30s with API key, 5 req/30s without | comprehensive CVE data, CWE mappings, references |
| GitHub Security Advisories (GHSA) | GraphQL API (`api.github.com/graphql`) or REST | 5000 req/hr with token | fix commits, affected versions, ecosystem tagging |
| Snyk Vulnerability DB | `snyk.io/vuln` (public pages, no stable API) | scrape-limited | npm/pip/go ecosystem advisories with code context |
| HackerOne Public Reports | `hackerone.com/hacktivity` (public) | scrape-limited | real-world exploitation context, proof-of-concept |
| npm Audit DB | `registry.npmjs.org/-/npm/v1/security/advisories` | no published limit | Node.js package-specific advisories |

### 1.4 Language Priority

1. TypeScript / JavaScript (Express, NestJS, Next.js, Fastify, Koa) -- primary
2. Python (Django, Flask, FastAPI) -- secondary
3. Go (Gin, Echo, net/http) -- tertiary
4. Java (Spring Boot) -- tertiary

---

## 2. CVE Mining Methodology

### Step 1: Query NVD API for candidate CVEs

Query the NVD API v2.0 filtered by CWE ID and keyword. Each query targets one CWE class at a time.

**API endpoint:** `https://services.nvd.nist.gov/rest/json/cves/2.0`

**Query parameters:**
- `cweId=CWE-89` (or target CWE)
- `keywordSearch=express+sequelize` (or relevant framework/library)
- `resultsPerPage=100`
- `startIndex=0` (paginate)
- `pubStartDate=2019-01-01T00:00:00.000` (focus on recent CVEs)

**Example request:**
```
GET https://services.nvd.nist.gov/rest/json/cves/2.0?cweId=CWE-89&keywordSearch=node.js&resultsPerPage=50
```

**Filtering criteria:**
1. CVE must have at least one reference URL pointing to a GitHub repository
2. CVE must map to a CWE in our target set
3. CVE must affect a package/application in a target language
4. CVSS v3.1 score >= 5.0 (medium or higher -- skip low-severity informational issues)

**Rate limiting:** use an NVD API key (free registration at `nvd.nist.gov/developers/request-an-api-key`). With a key: 50 requests per 30-second window. Without: 5 per 30s. The mining script must enforce this via a token-bucket rate limiter.

### Step 2: Locate the fix commit

For each candidate CVE:

1. **Check GHSA cross-references.** NVD entries typically include a GHSA link in the `references` array. Query the GitHub Advisory Database:
   ```
   gh api graphql -f query='{ securityAdvisory(ghsaId: "GHSA-xxxx-xxxx-xxxx") { vulnerabilities(first:5) { nodes { package { name ecosystem } firstPatchedVersion { identifier } } } references { url } } }'
   ```
2. **Search commit messages.** Clone the affected repo and search:
   ```
   git log --all --oneline --grep="CVE-2024-XXXXX"
   git log --all --oneline --grep="GHSA-xxxx"
   ```
3. **Diff tagged versions.** If the advisory specifies "fixed in version X.Y.Z":
   ```
   git log --oneline vX.Y.(Z-1)..vX.Y.Z
   ```
4. **Manual fallback.** If no commit is found programmatically, check the NVD references for blog posts, changelogs, or PR links that identify the fix.

**Output:** `(repo_url, vulnerable_commit, fix_commit)` tuple.

### Step 3: Extract vulnerable and fixed code

Given the fix commit, extract the relevant diff:

```bash
git diff <vulnerable_commit> <fix_commit> -- <affected_file>
```

Identify:
- **Taint source:** the user input entry point (e.g., `req.body.name`, `request.args.get("id")`)
- **Taint sink:** the dangerous API call (e.g., `db.query()`, `exec()`, `res.send()`)
- **Taint path:** intermediate variable assignments, function calls, and data transformations between source and sink

### Step 4: Create a minimal reproducible fixture

Strip the vulnerable code to its essential pattern:

1. Remove all business logic unrelated to the taint flow
2. Replace real database/service calls with stub interfaces (`declare function db.query(sql: string): any`)
3. Preserve the exact taint propagation pattern (assignments, function calls, conditionals)
4. Keep the fixture under 100 lines (ideally 30-60 lines)
5. Ensure the fixture parses cleanly (`npx tsc --noEmit` for TS, `python -c "import ast; ast.parse(open('f').read())"` for Python)

**Naming convention:** `eval/cve_fixtures/<cwe>-<cve_short>-<description>.ts`
- Example: `eval/cve_fixtures/cwe89-CVE-2023-26136-sequelize-raw-query.ts`
- Example: `eval/cve_fixtures/cwe79-CVE-2022-29078-ejs-render-opts.ts`

### Step 5: Annotate with `@piranesi-expect` comments

Each fixture must include inline annotations matching existing conventions:

```typescript
// @piranesi-expect CWE-89 source=req.body.query sink=db.query()
app.post("/search", async (req, res) => {
  const results = await db.query(`SELECT * FROM items WHERE name = '${req.body.query}'`); // taint sink
  res.json(results);
});
```

For safe/fixed versions, annotate with `@piranesi-expect-clean`:
```typescript
// @piranesi-expect-clean CWE-89
app.post("/search", async (req, res) => {
  const results = await db.query("SELECT * FROM items WHERE name = $1", [req.body.query]); // parameterized
  res.json(results);
});
```

### Step 6: Validate against Piranesi

Run Piranesi against the fixture and verify detection:

```bash
piranesi scan eval/cve_fixtures/cwe89-CVE-2023-26136-sequelize-raw-query.ts --format json
```

Record the result:
- **Detected:** Piranesi flagged the correct CWE at the correct sink location -> fixture is a valid TP ground truth entry
- **Missed:** Piranesi did not detect -> fixture exposes a detection gap, still valid GT entry (documents a known miss)
- **False alarm on fixed version:** Piranesi flagged the safe version -> fixture is a valid FP ground truth entry

### Automation: `eval/mine_cves.py`

Batch-query NVD and generate candidate fixtures. CLI interface:

```bash
# query NVD for CWE-89 CVEs related to Node.js, output candidate list
python eval/mine_cves.py query --cwe CWE-89 --keywords "node.js,express,sequelize" --since 2020-01-01 --output eval/candidates/cwe89.json

# given a candidate, attempt to extract the fix commit and generate a fixture stub
python eval/mine_cves.py extract --cve CVE-2023-26136 --repo https://github.com/sequelize/sequelize --output eval/cve_fixtures/

# batch process all candidates in a file
python eval/mine_cves.py batch --input eval/candidates/cwe89.json --output eval/cve_fixtures/ --max 50
```

---

## 3. Target CVE Categories

### Tier 1: Existing CWEs Needing Depth

These CWEs already have ground truth but need more real-world CVE-backed entries to improve detection accuracy.

#### CWE-89 SQL Injection
- **Current:** 64 entries (50 TP, 14 FP)
- **Target:** 80 entries (60 TP, 20 FP)
- **Mining targets:**
  - Sequelize advisories: CVE-2023-26136 (raw query interpolation), CVE-2023-22578 (replacements bypass)
  - TypeORM: GHSA-jcf2-mxr2-gmqp (query builder injection)
  - Prisma: CVE-2023-34108 (`$queryRaw` template literal misuse)
  - Knex.js: CVE-2016-20018 (`knex.raw()` with string concatenation)
  - pg (node-postgres): advisories around `query()` with string interpolation
  - Python: Django CVE-2022-28346 (`QuerySet.annotate()` injection), SQLAlchemy `text()` misuse
  - Go: GORM `Raw()` and `Exec()` with fmt.Sprintf patterns

#### CWE-79 Cross-Site Scripting
- **Current:** 57 entries (48 TP, 9 FP)
- **Target:** 75 entries (60 TP, 15 FP)
- **Mining targets:**
  - EJS: CVE-2022-29078 (server-side template injection via render options)
  - Pug/Jade: CVE-2021-21353 (unescaped interpolation)
  - React SSR: `dangerouslySetInnerHTML` with user input in Next.js server components
  - Express: `res.send()` / `res.write()` with unsanitized user input
  - Handlebars: CVE-2021-23383 (prototype pollution leading to XSS)
  - marked (Markdown): CVE-2022-21680 (ReDoS/XSS in markdown rendering)
  - Python: Django `mark_safe()` misuse, Jinja2 `|safe` filter on user input

#### CWE-78 OS Command Injection
- **Current:** 17 entries (13 TP, 4 FP)
- **Target:** 30 entries (22 TP, 8 FP)
- **Mining targets:**
  - npm packages using `child_process.exec()` with user input:
    - CVE-2021-21315 (systeminformation: command injection via SI.inetLatency)
    - CVE-2020-7646 (curlrequest: unsanitized URL)
    - CVE-2019-10061 (utils-extend: prototype pollution -> command injection)
  - Python: `subprocess.call(shell=True)` with f-strings, `os.system()` with user input
  - Go: `exec.Command` with unsanitized args (less common, but `sh -c` patterns exist)

#### CWE-22 Path Traversal
- **Current:** 24 entries (21 TP, 3 FP)
- **Target:** 40 entries (30 TP, 10 FP)
- **Mining targets:**
  - Express `express.static` misconfigurations
  - multer: CVE-2022-24434 (filename injection)
  - formidable: CVE-2022-29622 (arbitrary file upload path)
  - send (Express static middleware): CVE-2015-8859, path traversal via encoded dots
  - archiver/unzipper: zip slip variants (CVE-2018-1002204 pattern)
  - Python: `open(os.path.join(base, user_input))` without `os.path.realpath` check
  - Go: `filepath.Join` without `filepath.Rel` validation

### Tier 2: New CWEs from Phase 26-28 Roadmap

#### CWE-352 Cross-Site Request Forgery
- **Target:** 15 entries (10 TP, 5 FP)
- **Mining targets:**
  - Express apps missing `csurf` or custom CSRF token validation
  - Django views with `@csrf_exempt` decorator on state-changing endpoints
  - Rails: CVE-2023-28362 (CSRF token fixation)
  - Spring: missing `@CrossOrigin` + no CSRF token on POST handlers
  - FP patterns: SameSite cookie configurations, token validation middleware, read-only GET handlers

#### CWE-943 NoSQL Injection
- **Target:** 15 entries (10 TP, 5 FP)
- **Mining targets:**
  - MongoDB driver: `collection.find({ $where: userInput })` patterns
  - Mongoose: CVE-2023-3696 (query injection via operator pollution)
  - MongoDB `$gt`, `$ne`, `$regex` operator injection from JSON body parsing
  - Concrete pattern: `User.find({ username: req.body.username, password: req.body.password })` where `req.body.password` can be `{ "$gt": "" }`
  - FP patterns: Mongoose schema validation with `enum`/`match`, manual type checking before query

#### CWE-327 Use of Broken Cryptographic Algorithm
- **Target:** 12 entries (8 TP, 4 FP)
- **Mining targets:**
  - Node.js `crypto.createHash("md5")` or `crypto.createHash("sha1")` for password hashing
  - `crypto.createCipheriv("des-ecb", ...)` -- DES/3DES usage
  - `Math.random()` for token/secret generation (CWE-338 overlap)
  - JWT with `alg: "none"` or `HS256` with weak secret: CVE-2022-23529 (jsonwebtoken)
  - Python: `hashlib.md5(password)` without salt, `random.random()` for secrets
  - FP patterns: MD5/SHA1 used for non-security purposes (checksums, cache keys, ETags)

#### CWE-502 Unsafe Deserialization
- **Current:** 8 entries (7 TP, 1 FP)
- **Target:** 20 entries (15 TP, 5 FP)
- **Mining targets:**
  - node-serialize: CVE-2017-5941 (RCE via `unserialize()` with IIFE)
  - js-yaml: CVE-2013-4660 (`yaml.load()` with `!!js/function` tag)
  - Python pickle: `pickle.loads(user_input)` -- numerous CVEs in ML/data pipeline tools
  - Python yaml: `yaml.load(data)` without `Loader=SafeLoader` (CVE-2020-1747 in PyYAML)
  - Java: Jackson `enableDefaultTyping()` (CVE-2019-12384, CVE-2020-36518)
  - FP patterns: `JSON.parse()` (safe deserialization), `yaml.safe_load()`, schema-validated input

#### CWE-918 SSRF (expansion)
- **Current:** 23 entries (17 TP, 6 FP)
- **Target:** 35 entries (25 TP, 10 FP)
- **Mining targets:**
  - Axios/node-fetch with user-controlled URLs
  - CVE-2022-0155 (follow-redirects: SSRF via redirect to internal network)
  - PDF generators (puppeteer, wkhtmltopdf) fetching user-controlled URLs
  - Webhook URL validation bypass (DNS rebinding patterns)
  - Python: `requests.get(user_url)`, `urllib.request.urlopen(user_url)`
  - FP patterns: URL allowlist validation, protocol checks, private IP range filtering

#### CWE-200 Information Exposure
- **Target:** 10 entries (7 TP, 3 FP)
- **Mining targets:**
  - Express error handlers leaking stack traces in production (`app.use((err, req, res, next) => res.status(500).send(err.stack))`)
  - Verbose error messages exposing database schema or internal paths
  - API responses including sensitive fields (password hashes, tokens, internal IDs)
  - Python Django `DEBUG=True` in production
  - FP patterns: custom error handlers that sanitize messages, `NODE_ENV=production` checks

#### CWE-312 Cleartext Storage of Sensitive Information
- **Target:** 8 entries (6 TP, 2 FP)
- **Mining targets:**
  - Passwords stored in plaintext in database (no bcrypt/argon2)
  - API keys/secrets logged to stdout or written to files
  - Session tokens stored in localStorage (XSS-accessible)
  - FP patterns: bcrypt hashing before storage, encrypted-at-rest configurations

#### CWE-613 Insufficient Session Expiration
- **Target:** 8 entries (6 TP, 2 FP)
- **Mining targets:**
  - Express-session with no `maxAge` or excessively long `maxAge`
  - JWT with no `exp` claim or `expiresIn` set to years
  - Missing session invalidation on password change/logout
  - FP patterns: reasonable `maxAge` values, token refresh rotation

### Tier 3: Complex Patterns

#### Multi-Step Vulnerabilities (3+ taint steps)
- **Target:** 15 entries (12 TP, 3 FP)
- Patterns requiring taint to flow through 3+ intermediate functions/variables:
  - User input -> validation (partial) -> transformation -> database query
  - Request header -> middleware extraction -> service layer -> child_process
  - File upload -> temp storage -> path construction -> file operation
- Source CVEs: real-world Express/NestJS apps where taint flows cross middleware boundaries

#### Cross-Module Taint
- **Target:** 15 entries (12 TP, 3 FP)
- Patterns where taint flows across `import`/`require` boundaries:
  - Controller imports service, passes user input; service imports repository, builds query
  - Route handler imports helper module that wraps `exec()`
  - Middleware sets `req.locals`, downstream handler reads and uses unsafely
- Source: NestJS controllers -> services -> repositories pattern, Express route -> utility module

#### Framework-Specific Patterns
- **Target per framework:** 10 entries (8 TP, 2 FP)

| Framework | Language | Focus Patterns |
|-----------|----------|---------------|
| Express | TS/JS | middleware chains, `req.params`/`req.query`/`req.body`, `res.send`/`res.render` |
| NestJS | TS | `@Body()`, `@Query()`, `@Param()` decorators -> service methods -> TypeORM/Prisma |
| Next.js | TS | Server Actions (`"use server"`), API routes, `getServerSideProps` |
| Fastify | TS | `request.body`, `reply.send`, schema validation (FP when schema is strict) |
| Django | Python | `request.GET`/`request.POST`, `QuerySet.raw()`, `mark_safe()`, template `|safe` |
| Flask | Python | `request.args`/`request.form`, `render_template_string()`, `subprocess` |
| Spring Boot | Java | `@RequestParam`, `@PathVariable`, JdbcTemplate, `Runtime.exec()` |
| Gin | Go | `c.Query()`, `c.Param()`, `db.Raw()`, `exec.Command()` |

---

## 4. Ground Truth Schema Updates

### 4.1 New Fields

Extend `eval/ground_truth/schema.py` (`GroundTruthEntry`) with CVE provenance fields:

```python
class DiscoveryMethod(StrEnum):
    MANUAL = "manual"          # hand-audited from source application
    SYNTHETIC = "synthetic"    # hand-crafted fixture
    CVE_MINING = "cve_mining"  # extracted from real CVE

class Complexity(StrEnum):
    SIMPLE = "simple"
    INTERPROCEDURAL = "inter"
    CONTEXT_SENSITIVE = "ctx"
    CROSS_MODULE = "cross_module"   # new: spans import boundaries
    MULTI_STEP = "multi_step"       # new: 3+ taint steps

class GroundTruthEntry(BaseModel):
    # ... existing fields unchanged ...
    cve_id: str | None = None                # e.g., "CVE-2023-26136"
    ghsa_id: str | None = None               # e.g., "GHSA-wrh9-cjv3-2hfp"
    fix_commit: str | None = None            # commit SHA that fixed the vuln
    vulnerable_commit: str | None = None     # last vulnerable commit SHA
    patch_diff: str | None = None            # abbreviated diff showing the fix
    discovery_method: DiscoveryMethod = DiscoveryMethod.MANUAL
    language: str = "typescript"             # typescript, javascript, python, go, java
    framework: str | None = None             # express, nestjs, django, flask, gin, spring
    cvss_score: float | None = None          # CVSS v3.1 base score
    taint_step_count: int | None = None      # number of intermediate taint steps
```

### 4.2 Backward Compatibility

All new fields have default values (`None` or existing defaults). Existing 186 entries remain valid without modification. The YAML loader must tolerate missing new fields gracefully.

### 4.3 Updated YAML Example (CVE-mined entry)

```yaml
id: gt-187
source_project: sequelize
commit_hash: "abc123def456"
cwe_id: CWE-89
cwe_name: SQL Injection
label: true_positive
affected_files:
  - eval/cve_fixtures/cwe89-CVE-2023-26136-sequelize-raw-query.ts
line_numbers: [8, 12]
taint_source: req.body.query
taint_sink: sequelize.query()
taint_path:
  - "req.body.query (Express POST handler)"
  - "sequelize.query(`SELECT * FROM users WHERE name = '${req.body.query}'`)"
complexity: simple
exploitable: true
reference_exploit: "POST /search with body {\"query\": \"' OR 1=1 --\"} returns all users"
reference_fix_commit: "def789abc012"
notes: "Sequelize raw query with template literal interpolation. Fix: use bind parameters."
cve_id: "CVE-2023-26136"
ghsa_id: "GHSA-wrh9-cjv3-2hfp"
fix_commit: "def789abc012"
vulnerable_commit: "abc123def456"
patch_diff: |
  - sequelize.query(`SELECT * FROM users WHERE name = '${name}'`)
  + sequelize.query("SELECT * FROM users WHERE name = ?", { replacements: [name] })
discovery_method: cve_mining
language: typescript
framework: express
cvss_score: 9.8
taint_step_count: 1
```

### 4.4 ID Numbering

- CVE-mined TPs: continue from `gt-145` onward (gt-145, gt-146, ...)
- CVE-mined FPs: continue from `gt-fp-040` onward
- Cross-module entries: use standard numbering (no separate prefix)

---

## 5. Fixture Generation Tooling

All tools are standalone CLI scripts with no CI/CD or GitHub Actions dependency. They read/write local files and use HTTP APIs directly.

### 5.1 `eval/mine_cves.py` -- NVD Query + Candidate Generation

**Purpose:** Query NVD API v2.0, filter results, output a candidate list.

**Dependencies:** `httpx` (HTTP client), `pydantic` (data validation), stdlib `json`, `time`.

**CLI interface:**
```
usage: mine_cves.py [-h] {query,extract,batch} ...

subcommands:
  query     Query NVD for CVEs matching criteria
  extract   Extract fixture from a specific CVE
  batch     Process a candidate list end-to-end
```

**`query` subcommand:**
```bash
python eval/mine_cves.py query \
  --cwe CWE-89 \
  --keywords "sequelize,typeorm,prisma,knex" \
  --since 2020-01-01 \
  --min-cvss 5.0 \
  --output eval/candidates/cwe89-orm.json \
  --api-key "$NVD_API_KEY"  # optional, falls back to unauthenticated
```

**Output format (`eval/candidates/cwe89-orm.json`):**
```json
{
  "query": {"cwe": "CWE-89", "keywords": "sequelize,typeorm,prisma,knex", "since": "2020-01-01"},
  "queried_at": "2026-04-11T10:00:00Z",
  "total_results": 47,
  "candidates": [
    {
      "cve_id": "CVE-2023-26136",
      "cwe_ids": ["CWE-89"],
      "cvss_v31_score": 9.8,
      "description": "Sequelize before 6.19.1 allows SQL injection via raw query replacements...",
      "published": "2023-06-22",
      "references": [
        {"url": "https://github.com/sequelize/sequelize/pull/15375", "type": "patch"},
        {"url": "https://github.com/advisories/GHSA-wrh9-cjv3-2hfp", "type": "advisory"}
      ],
      "affected_package": "sequelize",
      "ecosystem": "npm",
      "status": "candidate"
    }
  ]
}
```

**Rate limiting implementation:**
```python
import time
class NvdRateLimiter:
    def __init__(self, has_api_key: bool):
        self.window = 30.0
        self.max_requests = 50 if has_api_key else 5
        self.timestamps: list[float] = []
    def wait(self):
        now = time.monotonic()
        self.timestamps = [t for t in self.timestamps if now - t < self.window]
        if len(self.timestamps) >= self.max_requests:
            sleep_time = self.window - (now - self.timestamps[0]) + 0.1
            time.sleep(sleep_time)
        self.timestamps.append(time.monotonic())
```

### 5.2 `eval/extract_fixture.py` -- Fixture Extraction

**Purpose:** Given a GitHub repo URL and commit range, clone the repo, extract the vulnerable code, and generate a minimal fixture.

**CLI interface:**
```bash
python eval/extract_fixture.py \
  --repo https://github.com/sequelize/sequelize \
  --vulnerable-commit abc123 \
  --fix-commit def456 \
  --affected-file src/dialects/abstract/query.js \
  --cwe CWE-89 \
  --output eval/cve_fixtures/cwe89-CVE-2023-26136-sequelize-raw-query.ts
```

**Workflow:**
1. Shallow-clone the repo to a temp directory (`git clone --depth=1` won't work for arbitrary commits; use `git fetch --depth=1 origin <sha>` or full clone with `--filter=blob:none`)
2. Checkout the vulnerable commit, extract the affected file(s)
3. Checkout the fix commit, extract the same file(s)
4. Generate a diff between vulnerable and fixed versions
5. Output a fixture stub with `TODO` markers where manual reduction is needed:

```typescript
// AUTO-GENERATED FIXTURE STUB -- requires manual reduction
// CVE: CVE-2023-26136 | CWE: CWE-89 | Package: sequelize
// Vulnerable commit: abc123 | Fix commit: def456
// Source file: src/dialects/abstract/query.js (lines 142-158)
//
// TODO: reduce to minimal taint flow (target < 100 lines)
// TODO: add @piranesi-expect annotation
// TODO: create corresponding safe/fixed version

// --- VULNERABLE CODE (from commit abc123) ---
// <extracted code here>

// --- FIX DIFF ---
// <patch diff here>
```

### 5.3 `eval/validate_fixture.py` -- Single Fixture Validation

**Purpose:** Run Piranesi against a fixture and check if detection matches the expected ground truth.

**CLI interface:**
```bash
python eval/validate_fixture.py \
  --fixture eval/cve_fixtures/cwe89-CVE-2023-26136-sequelize-raw-query.ts \
  --gt eval/ground_truth/gt-187.yaml \
  --verbose
```

**Output:**
```
gt-187 | CWE-89 | CVE-2023-26136 | PASS (detected at line 12, expected line 12)
```

Or:
```
gt-187 | CWE-89 | CVE-2023-26136 | FAIL (not detected -- expected CWE-89 at line 12)
```

### 5.4 `eval/validate_all.py` -- Batch Validation

**Purpose:** Run Piranesi against all ground truth entries and produce a detection report.

**CLI interface:**
```bash
# validate all entries
python eval/validate_all.py --gt-dir eval/ground_truth/ --fixtures-dir eval/ --output eval/validation_report.json

# validate only CVE-mined entries
python eval/validate_all.py --gt-dir eval/ground_truth/ --fixtures-dir eval/ --filter discovery_method=cve_mining

# CI mode: exit 1 if detection rate below threshold
python eval/validate_all.py --gt-dir eval/ground_truth/ --fixtures-dir eval/ --min-detection-rate 0.75 --min-fp-rate 0.80
```

**Report format (`eval/validation_report.json`):**
```json
{
  "timestamp": "2026-04-11T12:00:00Z",
  "piranesi_version": "0.2.0",
  "total_entries": 400,
  "results": {
    "overall": {
      "true_positives_detected": 280,
      "true_positives_total": 320,
      "detection_rate": 0.875,
      "false_positives_caught": 72,
      "false_positives_total": 80,
      "fp_suppression_rate": 0.90
    },
    "per_cwe": {
      "CWE-89": {"tp_detected": 55, "tp_total": 60, "rate": 0.917, "fp_caught": 18, "fp_total": 20},
      "CWE-79": {"tp_detected": 52, "tp_total": 60, "rate": 0.867, "fp_caught": 13, "fp_total": 15}
    },
    "per_complexity": {
      "simple": {"detected": 180, "total": 190, "rate": 0.947},
      "inter": {"detected": 60, "total": 70, "rate": 0.857},
      "ctx": {"detected": 20, "total": 25, "rate": 0.800},
      "cross_module": {"detected": 10, "total": 15, "rate": 0.667},
      "multi_step": {"detected": 10, "total": 20, "rate": 0.500}
    },
    "missed": [
      {"id": "gt-201", "cve_id": "CVE-2023-XXXXX", "cwe": "CWE-89", "reason": "taint lost at async boundary"}
    ]
  }
}
```

**Exit codes for CI compatibility:**
- `0`: all thresholds met
- `1`: detection rate below `--min-detection-rate`
- `2`: FP suppression rate below `--min-fp-rate`

---

## 6. Quality Criteria

### 6.1 Provenance

Every CVE-mined ground truth entry must be independently verifiable:
- `cve_id` links to `https://nvd.nist.gov/vuln/detail/<CVE-ID>`
- `ghsa_id` links to `https://github.com/advisories/<GHSA-ID>`
- `fix_commit` is a real commit SHA in the upstream repository
- `vulnerable_commit` is the parent of `fix_commit` (or the last known vulnerable commit)

### 6.2 Fixture Minimality

- Each fixture must be under 100 lines (target 30-60 lines)
- Must parse/compile cleanly:
  - TypeScript: `npx tsc --noEmit --strict` passes (with ambient type declarations)
  - Python: `python -c "import ast; ast.parse(open('file.py').read())"` succeeds
  - Go: `go vet ./...` passes
  - Java: `javac` compiles (with stub dependencies)
- Must contain only the code relevant to the taint flow -- no unrelated business logic

### 6.3 Paired Entries

Every CVE-mined TP entry should have a corresponding safe/fixed version where possible:
- Vulnerable fixture: `eval/cve_fixtures/cwe89-CVE-2023-26136-vuln.ts`
- Fixed fixture: `eval/cve_fixtures/cwe89-CVE-2023-26136-safe.ts`
- The safe version becomes an FP ground truth entry (Piranesi should NOT flag it)

### 6.4 Taint Path Completeness

Every entry must include a complete taint path from source to sink:
- Source must specify file and line: `"req.body.query (eval/cve_fixtures/cwe89-vuln.ts:5)"`
- Each intermediate step must specify the transformation: `"variable assignment (line 7)"`, `"function call to buildQuery() (line 12)"`
- Sink must specify the dangerous API: `"db.query(sql) (line 15)"`
- For cross-module flows, include the module boundary crossing: `"import boundary: controller.ts:8 -> service.ts:3"`

### 6.5 Deduplication

Before adding a new entry, verify it is not a duplicate of an existing pattern:
- Same CWE + same taint pattern (e.g., two `req.body -> db.query()` with string interpolation) -> skip unless the intermediate steps differ meaningfully
- Different CVE but identical code pattern -> keep only one, note the other CVE in `notes`
- Same CVE in different frameworks -> keep both (framework-specific detection matters)

---

## 7. Testing Integration

### 7.1 Unit Tests

Extend `tests/eval/` with:

```
tests/eval/test_gt_schema.py       # validate all YAML entries parse against GroundTruthEntry schema
tests/eval/test_gt_completeness.py # assert per-CWE minimums are met
tests/eval/test_fixture_syntax.py  # assert all fixtures parse cleanly
tests/eval/test_cve_provenance.py  # assert CVE-mined entries have cve_id, ghsa_id, fix_commit
```

### 7.2 `eval/validate_all.py` Thresholds

Default thresholds (configurable via CLI flags):

| Metric | Threshold | Description |
|--------|-----------|-------------|
| Overall TP detection rate | >= 75% | Piranesi must detect at least 75% of known TPs |
| Per-CWE TP detection rate | >= 60% | no single CWE falls below 60% detection |
| FP suppression rate | >= 80% | Piranesi must correctly suppress 80% of known FPs |
| Simple complexity detection | >= 85% | basic taint flows should rarely be missed |
| Interprocedural detection | >= 65% | cross-function flows are harder but must exceed baseline |
| Cross-module detection | >= 50% | hardest category, lower bar acceptable initially |

### 7.3 Regression Detection

When Piranesi changes cause a previously-detected GT entry to be missed:
1. `validate_all.py` logs the regression: `"REGRESSION: gt-187 was detected in v0.2.0 but missed in v0.2.1"`
2. Exit code 3 for regressions (distinct from threshold failures)
3. `eval/validation_report.json` includes a `regressions` array with entry IDs and versions

### 7.4 CI-Compatible Execution

All validation runs via standard CLI invocations with exit codes. No GitHub Actions dependency. Example integration in a Makefile:

```makefile
.PHONY: eval-validate
eval-validate:
	python eval/validate_all.py \
		--gt-dir eval/ground_truth/ \
		--fixtures-dir eval/ \
		--min-detection-rate 0.75 \
		--min-fp-rate 0.80 \
		--output eval/validation_report.json
```

---

## 8. Deliverables

### 8.1 Ground Truth Entries

| Category | New Entries | Total After Phase 29 |
|----------|------------|---------------------|
| CVE-mined TPs | ~120 | ~264 TPs |
| CVE-mined FPs | ~40 | ~79 FPs |
| Complex pattern TPs | ~30 | included in above |
| Framework-specific TPs | ~40 | included in above |
| **Total new** | **~200** | **~400+ entries** |

### 8.2 Fixture Files

| Directory | New Files | Description |
|-----------|----------|-------------|
| `eval/cve_fixtures/` | ~100 vulnerable + ~100 safe | CVE-mined minimal fixtures |
| `eval/synthetic/` | ~20 | new synthetic patterns for Tier 2/3 CWEs |

### 8.3 Mining Scripts

| Script | Purpose |
|--------|---------|
| `eval/mine_cves.py` | NVD API query, candidate generation, batch extraction |
| `eval/extract_fixture.py` | repo clone + diff extraction + fixture stub generation |
| `eval/validate_fixture.py` | single fixture validation against Piranesi |
| `eval/validate_all.py` | batch validation with thresholds and reporting |

### 8.4 Schema Updates

| File | Change |
|------|--------|
| `eval/ground_truth/schema.py` | add `cve_id`, `ghsa_id`, `fix_commit`, `vulnerable_commit`, `patch_diff`, `discovery_method`, `language`, `framework`, `cvss_score`, `taint_step_count`, `cross_module`/`multi_step` complexity values |

### 8.5 Validation Report

`eval/validation_report.json` produced by `validate_all.py` -- per-CWE detection rates, missed CVEs, false matches, regressions.

---

## 9. Acceptance Criteria

- [ ] 400+ total ground truth entries in `eval/ground_truth/`
- [ ] 20+ CWE classes represented
- [ ] At least 50 entries backed by real CVE IDs (not synthetic)
- [ ] All CVE-mined entries have `cve_id`, `ghsa_id` (where available), and `fix_commit`
- [ ] All fixtures parse cleanly in their target language
- [ ] `eval/mine_cves.py` successfully queries NVD API and generates candidate lists
- [ ] `eval/validate_all.py` runs to completion and produces a valid report
- [ ] Overall TP detection rate >= 75%
- [ ] No single Tier 1 CWE has detection rate < 60%
- [ ] FP suppression rate >= 80%
- [ ] All new entries follow the existing YAML schema (backward-compatible extension)

---

## 10. Concrete CVE Mining Targets (Starter List)

The following CVEs have been pre-identified as high-value mining targets with publicly available fix commits:

### CWE-89 SQL Injection
| CVE | Package | Language | CVSS | Fix Available |
|-----|---------|----------|------|--------------|
| CVE-2023-26136 | sequelize | JS | 9.8 | yes (GitHub PR #15375) |
| CVE-2023-22578 | sequelize | JS | 9.8 | yes |
| CVE-2023-34108 | @prisma/client | TS | 8.1 | yes |
| CVE-2016-20018 | knex | JS | 7.5 | yes |
| CVE-2022-28346 | Django | Python | 9.8 | yes (Django commit) |
| CVE-2023-30999 | SQLAlchemy | Python | 7.5 | yes |

### CWE-79 XSS
| CVE | Package | Language | CVSS | Fix Available |
|-----|---------|----------|------|--------------|
| CVE-2022-29078 | ejs | JS | 9.8 | yes |
| CVE-2021-23383 | handlebars | JS | 9.8 | yes |
| CVE-2021-21353 | pug | JS | 7.1 | yes |
| CVE-2022-21680 | marked | JS | 7.5 | yes |
| CVE-2023-29017 | vm2 | JS | 10.0 | yes (also CWE-94) |

### CWE-78 Command Injection
| CVE | Package | Language | CVSS | Fix Available |
|-----|---------|----------|------|--------------|
| CVE-2021-21315 | systeminformation | JS | 7.2 | yes |
| CVE-2020-7646 | curlrequest | JS | 9.8 | yes |
| CVE-2021-21307 | lucene-query-parser | JS | 9.8 | yes |
| CVE-2022-25883 | semver | JS | 7.5 | yes |

### CWE-22 Path Traversal
| CVE | Package | Language | CVSS | Fix Available |
|-----|---------|----------|------|--------------|
| CVE-2022-24434 | multer | JS | 7.5 | yes |
| CVE-2022-29622 | formidable | JS | 9.8 | yes |
| CVE-2017-16226 | static-eval | JS | 9.8 | yes |
| CVE-2018-1002204 | adm-zip | JS | 5.5 | yes (zip slip) |

### CWE-502 Deserialization
| CVE | Package | Language | CVSS | Fix Available |
|-----|---------|----------|------|--------------|
| CVE-2017-5941 | node-serialize | JS | 9.8 | yes |
| CVE-2013-4660 | js-yaml | JS | 9.8 | yes |
| CVE-2020-1747 | PyYAML | Python | 9.8 | yes |
| CVE-2020-36518 | jackson-databind | Java | 7.5 | yes |

### CWE-943 NoSQL Injection
| CVE | Package | Language | CVSS | Fix Available |
|-----|---------|----------|------|--------------|
| CVE-2023-3696 | mongoose | JS | 7.5 | yes |
| CVE-2021-20083 | mongoose | JS | 6.5 | yes |

---

## 11. Timeline

| Week | Milestone |
|------|-----------|
| 1 | Schema updates merged, `mine_cves.py` query subcommand working |
| 2 | 30 CVE candidates extracted, 15 fixtures manually reduced, `extract_fixture.py` working |
| 3 | 60 CVE-mined GT entries committed, Tier 1 CWEs at target depth |
| 4 | `validate_fixture.py` and `validate_all.py` working, first validation report |
| 5 | 100 new GT entries total, Tier 2 CWEs bootstrapped (10+ entries each) |
| 6 | 150 new entries, Tier 3 complex patterns added |
| 7-8 | 200+ new entries, all acceptance criteria met, validation report finalized |
