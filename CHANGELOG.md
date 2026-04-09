# Changelog

All notable changes to Piranesi are documented in this file.

## [0.1.0] - 2026-04-09

Initial alpha release.

### Added

- `piranesi run` pipeline orchestration across `scan`, `detect`, `triage`, `verify`, `legal`, `patch`, and `report`.
- Joern-backed transpilation, attack-surface extraction, and taint-flow detection for the current built-in source and sink set.
- Docker-backed verification flow with generated exploit payloads and reproducer scripts.
- JSON and Markdown report generation, including `report.json`, `report.md`, and `pr_body.md`.
- Example target app under `examples/vuln-express`.
- Example run documentation for the bundled Express app and OWASP NodeGoat under `docs/examples/`.
- `docs/getting-started.md` and `docs/configuration.md`.
- `SECURITY.md`.
- `--version` support at both `piranesi --version` and `piranesi version`.
- Apache 2.0 license metadata and repository URLs in `pyproject.toml`.

### Changed

- The pipeline now degrades gracefully when no LLM API key is configured:
  - Triage falls back to pass-through true positives with an explicit note.
  - Patch generation is skipped rather than failing the run.
- Sandbox execution now falls back to in-container requests when Docker internal networking does not expose a host port.
- TypeScript transpilation now sets `rootDir` in generated `tsconfig.json` files to avoid overwrite failures with modern `tsc`.
- The report markdown executive summary now renders clean bullet formatting.

### Fixed

- Added a working CLI version callback and version command.
- Fixed detect-stage wiring so the detector uses the correct category model arguments.
- Hardened condition extraction so unexpected Joern control-structure failures degrade to empty path conditions instead of aborting the finding.
- Narrowed scan entry-point queries to route methods instead of matching `app.use(...)`.
- Adjusted Joern control-structure queries to use valid CPG traversals.
- Added `.piranesi-out/` to `.gitignore`.

### Known Limitations

- The bundled vulnerable Express app currently yields 4 of 5 planted findings. The planted SQL injection is still missed.
- NodeGoat still produces a large SSRF false-positive cluster on `app.get(...)` and `app.post(...)` route registration.
- The full `piranesi run` path is less stable than the direct transpile-plus-detect helper on larger real-world apps such as NodeGoat.
- Without an LLM API key, triage, legal memo generation, and patch generation run in degraded mode.
