# Contributing

## Local Source Quickstart

```sh
make build
./bin/onibi agent install --agent claude
./bin/onibi start
```

Use the source-built `./bin/onibi` path for local development and docs changes.

## Checks

Run the local checks before opening a PR:

```sh
go test ./...
go vet ./...
make build
```

For frontend changes:

```sh
cd frontend
npm ci
bun run lint
bun run format:check
```

For security-sensitive changes, also run:

```sh
go test -race ./...
onibi system doctor --offline
```

## Reporting Security Issues

Use [SECURITY.md](SECURITY.md). Do not file public issues for exploitable bugs.

Report privately through GitHub Security Advisories:
<https://github.com/gongahkia/onibi/security/advisories/new>

## Adding a Transport

Start with [docs/transports.md](docs/transports.md) and [docs/transport-smoke.md](docs/transport-smoke.md).

Minimum PR checklist:

- Add the transport implementation and config/env validation.
- Add or update `onibi system doctor --transport <name>` checks.
- Add hermetic tests plus a live smoke path guarded by `ONIBI_LIVE_*` env vars.
- Document setup, cleanup, QR URL shape, and failure modes in `docs/transports.md`.

## Adding an Adapter

Start with [docs/SPEC-adapters.md](docs/SPEC-adapters.md) and [docs/adapters-registry.md](docs/adapters-registry.md).

Minimum PR checklist:

- Define the manifest schema or in-tree adapter entry, including hook install, verify, adopt, and uninstall behavior.
- Document the provider hook signature, event names, blocking/notify-only behavior, and trust step.
- Add install, uninstall, status, verify, adopt, and hook-deny tests when the provider can block tools.
- Add `onibi agent adapter validate <path>` output and registry evidence for community manifests.

Rules:

- Do not commit secrets, `.env`, SQLite state, logs, local cert keys, or hook output.
- Keep web cockpit actions behind owner-cookie authentication.
- Do not pass browser-provided text to a shell command except as literal PTY input.
- Prefer narrow changes over broad refactors.
- Update tests when changing pairing, approvals, rendering, service install, or socket intake.

Install the optional local pre-commit hook:

```sh
cp scripts/pre-commit.example .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```
