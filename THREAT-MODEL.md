# Threat Model

This file tracks top-level risks. Detailed controls live in [`docs/security.md`](./docs/security.md).

| # | adversary | capability | mitigation |
|---|---|---|---|
| T10 | Linux host without Secret Service | steals a powered-off disk and reads the SQLite master-key fallback file | not a defense; `<config-dir>/onibi/store.key` is 0600 but unencrypted at rest. Prefer an active credential store and full-disk encryption |
