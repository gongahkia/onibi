# Threat Model

This file tracks top-level risks. Detailed controls live in [`docs/security.md`](./docs/security.md).

| # | adversary | capability | mitigation |
|---|---|---|---|
| T10 | Linux host without Secret Service | steals a powered-off disk and reads the SQLite master-key fallback file | not a defense; `<config-dir>/onibi/store.key` is 0600 but unencrypted at rest. Prefer an active credential store and full-disk encryption |
| T11 | cross-site requester on a relay hostname | tries to drive owner-only writes with ambient cookies | owner mutating routes require `X-Onibi-CSRF`, minted via `/session-info` and bound to the owner session |
