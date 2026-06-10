## Summary


## Verification

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `make build`

## Security

- [ ] no tokens, `.env`, SQLite state, or logs committed
- [ ] Telegram input remains owner-checked before handling
- [ ] no Telegram-provided string is assembled into a shell command
- [ ] approval/session behavior has tests or a stated manual test
