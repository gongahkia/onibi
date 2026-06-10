# fish hooks

Installed by `onibi install-hooks --shell fish`. Writes
`~/.config/fish/conf.d/onibi.fish` and uses `fish_preexec`/`fish_postexec` to
emit `cmd_done` for commands longer than `$ONIBI_SHELL_MIN_MS` (default 5000).
