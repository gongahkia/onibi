## Summary

## Verification

- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `make build`

## Safety

- [ ] Telegram input remains literal tmux input; it is not assembled into a shell command.
- [ ] Codex `thread/shellCommand` is not exposed.
- [ ] Decision or session changes include focused tests.
- [ ] No token, state database, log, or terminal output is committed.
