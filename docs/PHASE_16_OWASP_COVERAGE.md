# Phase 16: OWASP Top 10 Full Coverage

**Estimated effort: 50-70 ideal hours**
**Blocked by: Phase 1 (taint analysis), Phase 8 (FP reduction)**
**Blocks: Nothing (incremental value)**
**Target milestone: v0.4.0**

---

## 1. Phase Overview

Piranesi currently detects 6 CWE classes (CWE-22, 78, 79, 89, 94, 918) covering OWASP A01 (Broken Access Control — partial), A03 (Injection), and A10 (SSRF). The OWASP Top 10 has 10 categories. This phase adds detection patterns for the remaining 7 categories that are detectable via static taint analysis.

Not all OWASP categories are amenable to SAST. A04 (Insecure Design) and A09 (Security Logging and Monitoring Failures) are largely design-level issues that require manual review. This phase focuses on what's statically detectable.

---

## 2. Secret Detection (A07 — Identification and Authentication Failures)

**Estimated effort: 10-12h**

### 2.1 Hardcoded Secret Patterns

Implement `src/piranesi/detect/secrets.py`:

| Pattern | Type | Example |
|---------|------|---------|
| `AKIA[0-9A-Z]{16}` | AWS Access Key | `AWS_ACCESS_KEY_REDACTED` |
| `sk_live_[a-zA-Z0-9]{24,}` | Stripe Secret Key | `sk_live_abc123...` |
| `ghp_[a-zA-Z0-9]{36}` | GitHub Personal Token | `ghp_abc123...` |
| `xox[bpors]-[a-zA-Z0-9-]{10,}` | Slack Token | `SLACK_TOKEN_REDACTED` |
| `SG\.[a-zA-Z0-9_-]{22}\.[a-zA-Z0-9_-]{43}` | SendGrid API Key | |
| `-----BEGIN (RSA\|EC\|DSA) PRIVATE KEY-----` | Private Key | PEM format |
| High-entropy strings (>4.5 Shannon entropy, >20 chars) | Generic secret | |

### 2.2 Integration

- Run secret detection as a sub-stage within `detect` (not a separate pipeline stage).
- Secrets produce `CandidateFinding` with `vuln_class: "CWE-798"` (Use of Hard-coded Credentials).
- Severity: CRITICAL for private keys and cloud provider keys, HIGH for API tokens.

### 2.3 Exclusions

- Skip `node_modules/`, `vendor/`, `.git/`
- Skip test files (`*.test.ts`, `*.spec.ts`) unless `--include-tests` flag
- Skip `.env.example` files (templates, not real secrets)
- Allow `.piranesi-ignore` suppression for known-safe patterns

---

## 3. Security Misconfiguration (A05)

**Estimated effort: 10-12h**

### 3.1 CORS Misconfiguration

Detect dangerous CORS configurations:

| Pattern | Risk | CWE |
|---------|------|-----|
| `Access-Control-Allow-Origin: *` with credentials | Credential exposure | CWE-942 |
| `origin` header reflected without validation | Origin bypass | CWE-346 |
| `Access-Control-Allow-Methods: *` | Overly permissive | CWE-942 |

Source/sink pairs in `scan/specs.py`:
- Source: `req.headers.origin`
- Sink: `res.setHeader('Access-Control-Allow-Origin', origin)` — reflected origin

### 3.2 Security Header Analysis

Detect missing security headers in response handlers:

| Missing Header | Risk | CWE |
|----------------|------|-----|
| `X-Frame-Options` | Clickjacking | CWE-1021 |
| `Content-Security-Policy` | XSS bypass | CWE-693 |
| `Strict-Transport-Security` | Downgrade attack | CWE-319 |
| `X-Content-Type-Options` | MIME sniffing | CWE-16 |

Implementation: scan for `res.send()`/`res.render()` calls and check if security headers are set on the response object upstream. This is an absence-of-pattern detection, not a taint flow.

### 3.3 Dangerous Defaults

| Pattern | Risk | CWE |
|---------|------|-----|
| `app.disable('x-powered-by')` missing | Information disclosure | CWE-200 |
| `cookie: { secure: false }` | Cookie theft | CWE-614 |
| `cookie: { httpOnly: false }` | XSS cookie access | CWE-1004 |
| `helmet()` not used | Missing security headers | CWE-693 |

---

## 4. Vulnerable Components (A06 — partially, A08)

**Estimated effort: 8-10h**

### 4.1 Dependency Vulnerability Integration

Implement `src/piranesi/detect/dependencies.py`:

Run package manager audit commands and parse output:
- `npm audit --json` for Node.js
- `pip-audit --format json` for Python
- `govulncheck -json ./...` for Go

### 4.2 Integration

- Run dependency check as a sub-stage within `scan`.
- Each vulnerable dependency produces a `CandidateFinding` with `vuln_class: "CWE-1395"` (Dependency on Vulnerable Third-Party Component).
- Include CVE ID, affected version, patched version, severity from advisory.

### 4.3 SBOM Generation

Optionally generate Software Bill of Materials:
- `--sbom spdx` → SPDX 2.3 JSON
- `--sbom cyclonedx` → CycloneDX 1.5 JSON

---

## 5. Unsafe Deserialization (A08)

**Estimated effort: 6-8h**

Detect unsafe deserialization patterns:

| Pattern | Language | CWE |
|---------|----------|-----|
| `JSON.parse(userInput)` without schema validation | JS/TS | CWE-502 |
| `yaml.load(userInput)` (unsafe loader) | Python | CWE-502 |
| `pickle.loads(userInput)` | Python | CWE-502 |
| `ObjectInputStream.readObject()` | Java | CWE-502 |
| `xml.Unmarshal(userInput)` without DTD disabled | Go | CWE-611 |

Source/sink pairs: user input → deserialization function without schema validation.

---

## 6. Open Redirect (A01 extension)

**Estimated effort: 4-5h**

| Pattern | CWE |
|---------|-----|
| `res.redirect(req.query.url)` | CWE-601 |
| `res.redirect(req.body.returnUrl)` | CWE-601 |
| `Location` header set from user input | CWE-601 |

---

## 7. Unrestricted File Upload (A04 extension)

**Estimated effort: 4-5h**

| Pattern | CWE |
|---------|-----|
| `multer()` without file type validation | CWE-434 |
| `req.file` passed to `fs.writeFile()` without extension check | CWE-434 |
| File extension from `req.file.originalname` used in path | CWE-434 |

---

## 8. Acceptance Criteria

- [ ] Secret detection: AWS keys, Stripe, GitHub tokens, private keys, high-entropy strings
- [ ] CORS: wildcard origin + credentials, reflected origin
- [ ] Security headers: detect missing X-Frame-Options, CSP, HSTS, X-Content-Type-Options
- [ ] Dangerous defaults: insecure cookies, missing helmet
- [ ] Dependency scanning: npm audit, pip-audit, govulncheck integration
- [ ] Unsafe deserialization: JSON.parse, yaml.load, pickle.loads
- [ ] Open redirect: res.redirect from user input
- [ ] File upload: multer without validation
- [ ] 20+ new ground truth entries for new CWE classes
- [ ] Coverage report: OWASP Top 10 mapping table in report output
