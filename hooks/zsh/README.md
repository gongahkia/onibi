# zsh hooks

Installed by `onibi install-hooks --shell zsh`. Appends a guarded
block to `~/.zshrc` that defines `preexec` (records command + start time)
and `precmd` (emits `cmd_done` event with exit status and elapsed time,
respecting the configured min-duration debounce — default 5s).

Off by default: enable only if you want shell-command-done Telegram
notifications in addition to agent-turn-complete notifications.
