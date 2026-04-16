# Prompt: Implement Helper and Wrapper Sink Promotion

You are working in the Piranesi repository, a Python 3.12+ AppSec CLI that detects security flows across source code and renders actionable reports. One important remaining detection gap is wrapper/helper promotion: if application code defines a helper such as `query(sql)` that internally calls a real sink such as `db.query(sql)`, calls to `query(userInput)` should be promoted as sink calls.

Goal: implement real helper/wrapper sink promotion for SQL injection and similar sink families so Piranesi detects taint flowing into local wrappers around dangerous APIs.

Relevant areas to inspect:

- `src/piranesi/detect/flows.py`
- `src/piranesi/detect/interprocedural.py`
- `src/piranesi/scan/specs.py`
- Existing detection tests under `tests/test_detect/`
- Existing source/sink specifications under `src/piranesi/scan/` and `rules/`

Implementation requirements:

- Discover local functions/methods that forward parameters into known sink APIs.
- Promote calls to those wrappers into effective sinks with enough metadata to explain why promotion occurred.
- Support simple direct forwarding first, for example `function run(sql) { return db.query(sql); }` and `const run = (sql) => pool.query(sql)`.
- Preserve original sink metadata and add wrapper metadata such as wrapper name, wrapper location, underlying sink, and forwarded argument index/name where available.
- Do not create broad false positives by treating every helper named `query` as a sink without evidence that it calls a known sink.
- Support deterministic behavior without LLM dependencies.
- Add tests that fail before the implementation and pass afterward, including a fake-Joern or fixture-based SQLi wrapper case.
- Update reports or explanations if useful so promoted sinks are transparent to users.

Acceptance criteria:

- Taint into a local wrapper around a known SQL sink is detected.
- Calls to unrelated helpers with sink-like names are not promoted.
- Promotion evidence is visible in debug/report artifacts or finding metadata.
- The implementation is extensible to other sink families beyond SQLi.

Validation suggestions:

- Run targeted detection tests.
- Run `python3 -m compileall -q src tests`.
- Run lint/format checks available in the repository.
