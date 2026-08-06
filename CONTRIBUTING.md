# Contributing

Build and verify the Telegram-first product locally:

```sh
make tidy
make vet
make test
make build
```

Manual smoke: configure a throwaway Telegram bot, start Onibi, pair one private chat, create a shell session, send a command, use `/screen`, test `/paste`, then stop the daemon. Do not use a real secret in test commands.

Keep scope narrow. Generic terminal programs get literal tmux I/O only; structured decision cards require documented agent events. Do not expose Codex `thread/shellCommand`, add browser/relay transports, or log terminal payloads.

Report security issues through [GitHub Security Advisories](https://github.com/gongahkia/onibi/security/advisories/new), not a public issue.
