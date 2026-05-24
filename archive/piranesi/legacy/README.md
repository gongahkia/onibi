# Legacy Quarantine

This tree contains pre-pivot Piranesi code, tests, fixtures, and documentation that are not
part of the active Phase 1 pentest/red-team report engine surface.

Active product code now lives under `src/piranesi/` and is limited to the workspace,
adapter, report, retest, signing, and local preview paths used by:

- `piranesi ingest`
- `piranesi report`
- `piranesi retest`
- `piranesi sign`
- `piranesi serve`

The explicit removal candidates from `WORKON-PIVOT-ASAP.md` were deleted instead of
quarantined:

- `verify/concolic.py`
- `detect/php_patterns.py`
- `detect/ruby_patterns.py`
- `detect/prototype_pollution.py`
- `detect/redos.py`

Everything else here is retained only as historical reference for potential future
phase work. It is not packaged, imported by active code, or exercised by active tests.
