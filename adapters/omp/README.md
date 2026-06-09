# OMP Adapter

OMP support is `native-observe`.

`onibi integration install omp` writes an observe-only extension to `~/.omp/agent/extensions/onibi.ts`, `OMP_CODING_AGENT_DIR/extensions/onibi.ts`, or `ONIBI_OMP_EXTENSION`.

The extension emits provider events to `/v1/adapters/omp/event`. It does not claim blocking approval coverage.
