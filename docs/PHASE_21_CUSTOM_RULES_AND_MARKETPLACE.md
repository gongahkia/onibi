# Phase 21: Custom Rule Authoring + Rule Distribution

**Estimated effort: 40-50 ideal hours**
**Blocked by: Phase 12 (plugin system), Phase 17 (community rules)**
**Blocks: Nothing (independent feature)**

## 1. Motivation

Built-in rules cover OWASP Top 10 but organizations have domain-specific security requirements: internal API patterns, proprietary framework sinks, compliance-specific checks. A rule authoring system lets security teams encode institutional knowledge without forking Piranesi.

Rule distribution via git repos enables sharing rules across teams and organizations without a centralized marketplace server — consistent with CLI-first philosophy.

## 2. Rule DSL — TOML Format

### 2.1 Rule Schema

```toml
[rule]
id = "custom-nosql-001"
name = "NoSQL Injection via MongoDB $where"
cwe_id = "CWE-943"
severity = "high"
description = "User input flows into MongoDB $where clause enabling arbitrary JS execution"
author = "security-team"
version = "1.0.0"
tags = ["nosql", "mongodb", "injection"]

[rule.source]
pattern = 'cpg.call.name("<operator>.fieldAccess").code("req[.]body.*|req[.]query.*|req[.]params.*")'
type = "cpgql" # "cpgql" or "regex"

[rule.sink]
pattern = 'cpg.call.name("find|findOne|aggregate").argument.code(".*\\$where.*")'
type = "cpgql"

[rule.sanitizers]
patterns = [
    'cpg.call.name("sanitize|escape|validator[.]isAlphanumeric")',
]

[rule.message]
template = "User input from `{source}` flows to MongoDB $where clause at `{sink}`, enabling server-side JavaScript injection"

# optional: extend a built-in rule
# extends = "builtin:sqli"
# override_severity = "critical"
# additional_sanitizers = ["mongo-sanitize"]
```

### 2.2 Pattern Types

**CPGQL patterns** (`type = "cpgql"`): executed directly against Joern CPG. Most powerful, requires Joern knowledge.

**Regex patterns** (`type = "regex"`): matched against source code text. Simpler, no Joern required, but no taint tracking — only detects presence of patterns.

### 2.3 Rule Inheritance

Rules can extend built-in rules:
```toml
[rule]
id = "custom-sqli-stricter"
extends = "builtin:sqli"
override_severity = "critical"

[rule.additional_sanitizers]
patterns = ["company_sanitize_sql"]
```

This inherits the base rule's source/sink patterns and adds custom sanitizers or overrides severity.

## 3. Rule Testing Framework

### 3.1 Inline Tests

Each rule TOML file can embed test cases:

```toml
[[tests]]
fixture = "tests/fixtures/nosql_injection.ts"
expect_finding = true
expect_cwe = "CWE-943"
expect_source_line = 12
expect_sink_line = 15

[[tests]]
fixture = "tests/fixtures/nosql_safe.ts"
expect_finding = false
description = "Parameterized query should not trigger"
```

### 3.2 CLI Commands

```
piranesi rules validate <path>          # syntax + pattern validation
piranesi rules test <path> --fixture <dir>  # run single rule against fixture
piranesi rules test-all                 # run all rule inline tests
piranesi rules coverage                 # CWE coverage report
```

### 3.3 Validation Checks

- Required fields present (id, name, cwe_id, severity, source, sink)
- CPGQL pattern syntax valid (dry-run parse)
- Regex pattern compiles
- CWE ID format valid
- Severity in {low, medium, high, critical}
- No duplicate rule IDs across all loaded rules

## 4. Rule Distribution via Git

### 4.1 Install/Update/Remove

```
piranesi rules install https://github.com/org/piranesi-rules.git
piranesi rules install git@github.com:org/piranesi-rules.git --name org-rules
piranesi rules update                   # git pull all installed repos
piranesi rules update org-rules         # update specific repo
piranesi rules remove org-rules
piranesi rules list                     # show installed rule sets
```

Rules installed to `~/.piranesi/rules/<name>/`. Each repo is a directory of `.toml` rule files.

### 4.2 Configuration

```toml
[rules]
paths = ["./rules", "~/.piranesi/rules/*"]
disabled_rules = ["noisy-rule-001", "org-rules:experimental-*"]
```

### 4.3 Namespacing

Installed rules are namespaced: `<repo-name>:<rule-id>`. This prevents collisions between different rule sets. Local rules (in `./rules/`) have no namespace prefix.

### 4.4 Rule Repo Structure

```
piranesi-rules/
├── rules/
│   ├── nosql-injection.toml
│   ├── ldap-injection.toml
│   └── xml-injection.toml
├── tests/
│   └── fixtures/
│       ├── nosql_injection.ts
│       └── nosql_safe.ts
├── README.md
└── piranesi-rules.toml  # metadata: name, version, description, min_piranesi_version
```

### 4.5 Optional Signature Verification

```toml
[rules]
require_signatures = false  # default
trusted_keys = ["~/.piranesi/trusted-keys/"]
```

When `require_signatures = true`, rule repos must have a GPG-signed tag matching the installed version.

## 5. Integration with Pipeline

### 5.1 Rule Loading Order

1. Built-in specs from `scan/specs.py` (always loaded)
2. Local rules from `./rules/` directory
3. Installed rules from `~/.piranesi/rules/*/`
4. Custom source/sink patterns from `piranesi.toml` `[scan.custom_sources]`

### 5.2 Execution

CPGQL rules execute alongside built-in specs in the detect stage. Regex rules execute as a pre-pass before Joern analysis (no CPG required).

Custom rule findings go through the same triage → verify → legal → patch pipeline as built-in findings.

## 6. Tests

1. Write 3 custom rules: NoSQL injection (CWE-943), LDAP injection (CWE-90), XML external entity (CWE-611).
2. Each rule has inline test fixtures (positive + negative).
3. Test `piranesi rules validate` catches malformed rules.
4. Test `piranesi rules test` reports correct pass/fail.
5. Test `piranesi rules install` with a mock git repo.
6. Test rule namespacing prevents collisions.
7. Test `disabled_rules` config properly excludes rules.

## 7. Risks

- **CPGQL injection**: malicious rule TOML could contain harmful CPGQL. Mitigation: CPGQL is read-only (queries, not mutations). Validate patterns don't contain write operations.
- **Rule quality**: community rules may have high false positive rates. Mitigation: rule testing framework, coverage reports.
- **Version compatibility**: rules may break with Piranesi updates. Mitigation: `min_piranesi_version` field in rule repo metadata.
