# Contributing

## Local Source Quickstart

```sh
make build
./bin/onibi install-hooks --agent claude
./bin/onibi up
```

Use the source-built `./bin/onibi` path for local development and docs changes.

## Checks

Run the local checks before opening a PR:

```sh
go test ./...
go vet ./...
make build
```

For security-sensitive changes, also run:

```sh
go test -race ./...
onibi doctor --offline
```

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
