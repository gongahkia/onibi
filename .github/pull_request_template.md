## Summary


## Verification

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `make build`

## Security

- [ ] This PR does NOT change security-affecting surface (auth, TLS, cryptographic protocols, hook execution, PTY). If checked, hook-installation smoke run: yes/no
- [ ] no tokens, `.env`, SQLite state, or logs committed
- [ ] Telegram input remains owner-checked before handling
- [ ] no Telegram-provided string is assembled into a shell command
- [ ] approval/session behavior has tests or a stated manual test

Security reports: [SECURITY.md](https://github.com/gongahkia/onibi/blob/main/SECURITY.md)

Contribution guides: [Adding a Transport](https://github.com/gongahkia/onibi/blob/main/CONTRIBUTING.md#adding-a-transport), [Adding an Adapter](https://github.com/gongahkia/onibi/blob/main/CONTRIBUTING.md#adding-an-adapter), [Adding a Provider](https://github.com/gongahkia/onibi/blob/main/CONTRIBUTING.md#adding-a-provider)
