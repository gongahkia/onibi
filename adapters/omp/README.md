# OMP Adapter

OMP support is `native-blocking`.

`onibi integration install omp` writes a blocking extension to `~/.omp/agent/extensions/onibi.ts`, `OMP_CODING_AGENT_DIR/extensions/onibi.ts`, or `ONIBI_OMP_EXTENSION`.

The extension emits provider events to `/v1/adapters/omp/event` and blocks `tool_call` execution through Onibi. Edited-input forwarding is not enabled because OMP's extension contract exposes blocking, not a verified input-rewrite path.
