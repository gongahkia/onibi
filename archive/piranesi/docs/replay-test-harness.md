# Replay Test Harness

The deterministic replay harness exercises `piranesi rescan` without requiring Docker in the
default test lane. Tests provide a fixture-backed container runner that writes known nmap or nuclei
outputs, then the harness ingests those replay outputs and compares normalized findings against an
expected workspace.

The harness normalizes fields that are expected to vary between runs:

- `first_seen` and `last_seen` timestamps;
- raw workspace paths inside source references.

It keeps stable finding IDs, severities, affected assets, evidence snippets, source digests, and
provenance fields in the comparison. A mismatch raises `ReplayHarnessError`.

Default CI should run this non-Docker path with the normal unit suite. Tests that exercise a real
Docker daemon should use the existing `docker` pytest marker so they are skipped outside the
optional integration lane.
