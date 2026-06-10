# bash hooks

Installed by `onibi install-hooks --shell bash`. Appends a guarded block to
`~/.bashrc` using `trap DEBUG` + `PROMPT_COMMAND` to emit `cmd_done` for
commands longer than `$ONIBI_SHELL_MIN_MS` (default 5000).
