# Contributing

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

- Do not commit bot tokens, `.env`, SQLite state, logs, or local hook output.
- Keep Telegram update handling behind the owner-check router.
- Do not pass Telegram-provided text to a shell command.
- Prefer narrow changes over broad refactors.
- Update tests when changing pairing, approvals, rendering, service install, or socket intake.

Install the optional local pre-commit hook:

```sh
cp scripts/pre-commit.example .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```
