# Example: OWASP NodeGoat

This example documents the release-validation run against OWASP NodeGoat.

## Target

- Repository: <https://github.com/OWASP/NodeGoat>
- Local path used during release prep: `workspace/nodegoat/app`
- Commit used for the documented run: `c5cb68a7084e4ae7dcc60e6a98768720a81841e8`

## Setup

From the repository root:

```bash
git clone https://github.com/OWASP/NodeGoat.git workspace/nodegoat
```

No package install was required for the documented detect run because Piranesi only needed the checked-out source tree.

## Invocation Used For `v0.2.0`

The most reproducible current path on NodeGoat is the direct transpile-plus-detect helper:

```bash
uv run python docs/examples/run_detect_summary.py workspace/nodegoat/app --show-limit 16
```

Why this was used instead of `piranesi run`:

- The full `piranesi run` path is still brittle on NodeGoat-sized targets in `v0.2.0`.
- The helper exercises the same Joern-backed transpile and detect logic, but avoids the noisier scan-stage attack-surface path that currently fails more often on NodeGoat.

## Representative Output

```text
subprocess_exec failed | on=tsc --project /tmp/.../tsconfig.json | why=exit_code=1 | debug=stdout=error TS5055: Cannot write file '/Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/config/config.js' because it would overwrite input file.
TypeScript transpilation reported errors; retrying with forced emit flags
Joern port unavailable, trying next candidate
Joern port unavailable, trying next candidate
Joern port unavailable, trying next candidate
Piranesi Detect Summary
Target: /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app
Transpile failures tolerated: 0
Candidate findings: 32
By CWE:
  CWE-79: 12
  CWE-918: 17
  CWE-94: 3
Findings:
  - CWE-94 | source=roth | sink=eval | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/contributions.js:34
  - CWE-94 | source=afterTax | sink=eval | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/contributions.js:33
  - CWE-94 | source=preTax | sink=eval | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/contributions.js:32
  - CWE-79 | source=preTax | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/contributions.js:70
  - CWE-79 | source=body | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/session.js:249
  - CWE-79 | source=roth | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/contributions.js:70
  - CWE-79 | source=body | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/session.js:82
  - CWE-79 | source=body | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/profile.js:100
  - CWE-79 | source=afterTax | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/contributions.js:70
  - CWE-79 | source=body | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/session.js:239
  - CWE-79 | source=body | sink=res.render | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/profile.js:65
  - CWE-918 | source=body | sink=app.post | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/index.js:52
  - CWE-918 | source=body | sink=app.post | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/index.js:48
  - CWE-918 | source=body | sink=app.get | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/index.js:63
  - CWE-918 | source=body | sink=app.get | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/index.js:66
  - CWE-918 | source=body | sink=app.get | /Users/gongahkia/Desktop/coding/projects/piranesi/workspace/nodegoat/app/routes/index.js:76
  ... 16 more
```

## What Was Found

Clear true positives:

- `CWE-94` server-side JavaScript injection in `routes/contributions.js`, where `req.body.preTax`, `req.body.afterTax`, and `req.body.roth` all flow into `eval(...)`.
- Multiple `CWE-79` candidate flows into `res.render(...)` in `routes/session.js`, `routes/profile.js`, and `routes/contributions.js`.

These were materially useful detections: they point directly at known vulnerable NodeGoat code that a reviewer would want to inspect first.

## What Was Missed

Important known misses:

- The NoSQL injection in `data/allocations-dao.js`, reachable from `routes/allocations.js`, was not surfaced. NodeGoat builds a Mongo `$where` expression from `req.query.threshold`, and that pattern is not currently modeled as a sink.
- The open redirect in `routes/index.js` (`res.redirect(req.query.url)`) was not surfaced.
- Session-management and access-control issues in NodeGoat are also outside the current sink coverage.

## False Positives

The main false-positive cluster was obvious:

- 17 findings were labeled `CWE-918` even though the sink was `app.get(...)` or `app.post(...)` in `routes/index.js`.
- Those are route registrations, not outbound HTTP requests.
- This came from the current SSRF sink pattern matching `get|post|request` too broadly.

This is the largest release blocker exposed by the NodeGoat example.

## Timing

The timed release run completed in:

- `real 18.90s`

The helper currently reports a compact detect summary rather than per-stage timings.

## Takeaway

NodeGoat is a useful alpha validation target because it shows both sides of the tool:

- Real signal: the `eval(...)` findings and several render-path findings are legitimate.
- Real noise: the SSRF sink model is too broad on Express route registration.
- Real misses: `$where`/Mongo-style injection is not yet modeled.

That is exactly the kind of honest release documentation an alpha security tool should ship with.
