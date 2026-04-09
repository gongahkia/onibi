# Joern Validation Spike Report

Date: 2026-04-09

## Executive Summary

This spike tested whether Joern is viable as the taint-analysis backend for Piranesi Phase 1.

- JVM requirement passed: `java -version` reported OpenJDK `17.0.18`.
- Joern was installed with Homebrew. The current CLI build does not support `joern --version`; the startup banner reported `HEAD+20260325-0833`.
- All TypeScript projects were transpiled with a Piranesi-owned config derived from [`eval/joern_spike/piranesi-tsconfig.template.json`](/Users/gongahkia/Desktop/coding/projects/piranesi/eval/joern_spike/piranesi-tsconfig.template.json), never the target repo's `tsconfig.json`.

Result:

- Baseline generic query coverage: `2 / 4` benchmark flow families detected = `50%`
- After one targeted query improvement for SQL wrapper sinks: `3 / 4` detected = `75%`
- Large-project latency projected to 500 files: `55.97s` Joern-only, `61.63s` including transpilation

Recommendation: **NO-GO for immediate Phase 1 commitment.**

Reason: coverage improved into the `60-80%` band, but remained below the `>= 80%` success gate, and the remaining miss was a direct `req.query -> ChildProcess.exec/spawn` flow in a real Express microservice. That miss did not close with narrower source selection or receiver-aware sink name matching.

## Method

Harness files:

- [`eval/joern_spike/piranesi-tsconfig.template.json`](/Users/gongahkia/Desktop/coding/projects/piranesi/eval/joern_spike/piranesi-tsconfig.template.json)
- [`eval/joern_spike/suite.sc`](/Users/gongahkia/Desktop/coding/projects/piranesi/eval/joern_spike/suite.sc)

Transpile command shape:

```sh
npx -y -p typescript@5.8.3 tsc -p /tmp/<repo>/piranesi.spike.tsconfig.json
```

Joern import command shape:

```sh
joern-parse /tmp/spike-<name> -o /tmp/spike-<name>.cpg.bin --language javascript
```

Baseline Joern query categories:

- SQLi: `req.body -> query() / $queryRaw()`
- Command injection: `req.query -> exec() / spawn()`
- XSS: `req.params -> res.send() / res.render()`
- Path traversal: `req.body -> readFile() / writeFile()`

## Benchmark Set

| Repo | Shape | JS files | Ground-truth benchmark flow(s) | Outcome |
|---|---|---:|---|---|
| `nonhana/apiplayer-backend` | Simple REST API | 25 | `req.body -> queryPromise(...)` wrapper, e.g. [`controller/users/index.ts:65`](/tmp/piranesi-joern-candidates/apiplayer-backend/controller/users/index.ts#L65) to [`controller/users/index.ts:69`](/tmp/piranesi-joern-candidates/apiplayer-backend/controller/users/index.ts#L69), and [`controller/projects/index.ts:65`](/tmp/piranesi-joern-candidates/apiplayer-backend/controller/projects/index.ts#L65) to [`controller/projects/index.ts:68`](/tmp/piranesi-joern-candidates/apiplayer-backend/controller/projects/index.ts#L68) | Baseline miss. Wrapper-aware sink query detected flows. |
| `Louis3797/express-ts-auth-service` | Prisma-backed auth + middleware | 29 | Negative control. Request surfaces are sanitized in [`src/middleware/xssMiddleware.ts:6`](/tmp/piranesi-joern-candidates/express-ts-auth-service/src/middleware/xssMiddleware.ts#L6) to [`src/middleware/xssMiddleware.ts:10`](/tmp/piranesi-joern-candidates/express-ts-auth-service/src/middleware/xssMiddleware.ts#L10) | No benchmark-family hits, no observed false positives. |
| `shisama/typed-express-sample` | Minimal Express app | 4 | `req.params.user_id -> res.send(...)` at [`src/routes/users.ts:9`](/tmp/piranesi-joern-candidates/typed-express-sample/src/routes/users.ts#L9) to [`src/routes/users.ts:10`](/tmp/piranesi-joern-candidates/typed-express-sample/src/routes/users.ts#L10) | Detected. |
| `AdMetaNetwork/admeta-hackathon` | Next.js API routes | 13 | `req.body.key -> fs.writeFile(...)` and `req.body.key -> fs.readFile(...)` at [`pages/api/upload.ts:30`](/tmp/piranesi-joern-candidates/admeta-hackathon/pages/api/upload.ts#L30) to [`pages/api/upload.ts:39`](/tmp/piranesi-joern-candidates/admeta-hackathon/pages/api/upload.ts#L39) | Detected. |
| `orchid-ltd/orchid_ddm` | Async-heavy microservice | 922 | `req.query.cli/args -> ChildProcess.exec/spawn(...)` at [`src/browser/webapps.ts:160`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L160) to [`src/browser/webapps.ts:166`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L166); extra stress test: `req.query.path -> StorageManager.read/write(...)` at [`src/browser/webapps.ts:250`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L250) to [`src/browser/webapps.ts:256`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L256), with underlying fs sinks in [`src/storage/index.ts:34`](/tmp/piranesi-joern-candidates/orchid_ddm/src/storage/index.ts#L34) to [`src/storage/index.ts:57`](/tmp/piranesi-joern-candidates/orchid_ddm/src/storage/index.ts#L57) | Missed. |

Upstream repos:

- `https://github.com/nonhana/apiplayer-backend`
- `https://github.com/Louis3797/express-ts-auth-service`
- `https://github.com/shisama/typed-express-sample`
- `https://github.com/AdMetaNetwork/admeta-hackathon`
- `https://github.com/orchid-ltd/orchid_ddm`

## Detection Results

### Baseline generic queries

| Repo | SQLi | Command Injection | XSS | Path Traversal | Notes |
|---|---|---|---|---|---|
| `apiplayer-backend` | Miss | N/A | N/A | N/A | `sinks=0`, because the sink is a local wrapper (`queryPromise`) instead of a direct `query()` call. |
| `express-ts-auth-service` | N/A | N/A | N/A | N/A | Negative control; no target-family detections. |
| `typed-express-sample` | N/A | N/A | Hit | N/A | `flows=2` in transpiled `routes/users.js`. |
| `admeta-hackathon` | N/A | N/A | N/A | Hit | `flows=11` in transpiled `pages/api/upload.js`. |
| `orchid_ddm` | N/A | Miss | N/A | Miss | Direct command flow missed; wrapper path flow also missed. |

Baseline detection rate on the benchmark flow families: **`2 / 4 = 50%`**

Counted benchmark families:

1. SQL wrapper flow in `apiplayer-backend`
2. XSS flow in `typed-express-sample`
3. Path-traversal flow in `admeta-hackathon`
4. Command-injection flow in `orchid_ddm`

### Query-improved rerun

For `apiplayer-backend`, changing the SQL sink from direct `name("query|$queryRaw")` matching to a wrapper-aware sink regex (`.*queryPromise` on both name/code) changed the result from a miss to a hit:

- baseline: `sources=93 sinks=0 flows=0`
- wrapper-aware rerun: `sources=93 sinks=348 flows=175`

This raised overall benchmark-family coverage to: **`3 / 4 = 75%`**

## False Positives

Two different views matter here:

- **Benchmark-family false positives:** `0`
- **Raw flow-path noise:** high, especially on wrapper-aware SQL

Observed noise:

- `apiplayer-backend` wrapper-aware SQL query produced `175` raw flows for a small number of logical families.
- `admeta-hackathon` path-traversal query produced `11` raw flows for one handler.

Interpretation:

- Joern can produce many duplicate or alias-expanded flow rows for one logical issue.
- Safe parameterized SQL calls such as [`controller/users/index.ts:69`](/tmp/piranesi-joern-candidates/apiplayer-backend/controller/users/index.ts#L69) would still need Piranesi-side sanitization and deduplication logic before surfacing findings.

## Latency

| Repo | JS files | Transpile (s) | Joern parse (s) | Query suite (s) | Notes |
|---|---:|---:|---:|---:|---|
| `apiplayer-backend` | 25 | 1.10 | 8.39 | 11.95 | Wrapper-aware rerun took `8.38s` |
| `express-ts-auth-service` | 29 | 1.05 | 7.38 | 11.90 | Negative control |
| `typed-express-sample` | 4 | 0.81 | 5.29 | 11.53 | Smallest codebase |
| `admeta-hackathon` | 13 | 1.06 | 7.83 | 12.14 | Direct fs sinks |
| `orchid_ddm` | 922 | 10.44 | 56.78 | 46.43 | Large async-heavy case |

Large-project projection from the 922-file `orchid_ddm` run:

- Parse-only at 500 files: `30.79s`
- Query-suite-only at 500 files: `25.18s`
- Joern parse + query at 500 files: `55.97s`
- End-to-end transpile + parse + query at 500 files: `61.63s`

Latency conclusion:

- **Joern-only latency narrowly meets the `< 60s / 500 files` target**
- **End-to-end latency including transpilation narrowly misses it**

## Undetectable Patterns And Root Causes

### 1. Local SQL wrappers are not visible to naive sink queries

Example:

- [`controller/users/index.ts:65`](/tmp/piranesi-joern-candidates/apiplayer-backend/controller/users/index.ts#L65) to [`controller/users/index.ts:69`](/tmp/piranesi-joern-candidates/apiplayer-backend/controller/users/index.ts#L69)

Observed behavior:

- The transpiled call is modeled as `(0, utils_1.queryPromise)(...)`
- Baseline `call.name("query|\\$queryRaw")` did not match it

Root cause: **query gap**

Assessment:

- This gap is closable with receiver-aware or project-configurable sink templates.
- It increases noise and will require custom wrapper modeling plus deduplication.

### 2. Direct `req.query -> ChildProcess.exec/spawn` flows were not recovered

Example:

- [`src/browser/webapps.ts:160`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L160) to [`src/browser/webapps.ts:166`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L166)

Observed behavior:

- Receiver-aware sink regex found the sinks: `sources=96 sinks=4`
- Narrowing the source to exact `req.query.cli` and `req.query.args` still produced `sources=8 sinks=4 flows=0`

Root cause: **likely Joern JS dataflow limitation, not just query authoring**

Assessment:

- This was a direct same-line flow, so missing it is a serious concern.
- The miss survived both broader and narrower source selection.

### 3. Wrapper-mediated file flows were not recovered interprocedurally

Examples:

- [`src/browser/webapps.ts:250`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L250) to [`src/browser/webapps.ts:256`](/tmp/piranesi-joern-candidates/orchid_ddm/src/browser/webapps.ts#L256)
- [`src/storage/index.ts:34`](/tmp/piranesi-joern-candidates/orchid_ddm/src/storage/index.ts#L34) to [`src/storage/index.ts:57`](/tmp/piranesi-joern-candidates/orchid_ddm/src/storage/index.ts#L57)

Observed behavior:

- Sink selection succeeded: `sources=96 sinks=51`
- `flows=0`

Root cause: **likely interprocedural/wrapper limitation**

Assessment:

- This was treated as an extra stress test, not part of the core 4-family score.
- It reduces confidence in Joern for realistic service code that routes request data through helper abstractions.

## Recommendation

**Recommendation: NO-GO for immediate Phase 1 commitment.**

Why:

- The spike did not clear the `>= 80%` coverage gate.
- Best result after query tuning was `75%`.
- The remaining miss was not a minor naming issue. It was a direct `req.query -> ChildProcess.exec/spawn` flow in production-style Express code.

What this means for Piranesi:

- Joern is promising for direct JS/Express flows and for simple wrapper adaptation.
- Joern is not yet reliable enough, based on this spike alone, to be adopted as the sole taint backend for Piranesi Phase 1.

If the team wants one short follow-up before abandoning Joern, the highest-value next experiment is:

1. Rebuild the source queries around semantic traversals such as `method.parameter.name("req").dotAccess(...)` instead of regex-only `call.code(...)`
2. Add first-class sink templates for receiver-qualified calls and local wrappers
3. Re-run only the `orchid_ddm` command-injection benchmark

If that follow-up still misses the orchid command flow, the backend choice should be escalated and Joern should be treated as **not viable** for Phase 1.
