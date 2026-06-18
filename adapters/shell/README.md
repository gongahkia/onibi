# Shell Hooks

Install:

```sh
onibi install-hooks --shell zsh
onibi install-hooks --shell bash
onibi install-hooks --shell fish
```

Inspect:

```sh
onibi hooks --show --shell zsh
```

Paths:

- zsh: `~/.zshrc`
- bash: `~/.bashrc`
- fish: `~/.config/fish/conf.d/onibi.fish`

Behavior:

- emits `cmd_done` for commands longer than `shell.min_duration`
- override per shell session with `ONIBI_SHELL_MIN_MS`
- edit threshold with `onibi config --set shell.min_duration <duration>`, then reinstall the shell hook

Compatibility:

- zsh uses `add-zsh-hook preexec/precmd`; reinstall if oh-my-zsh/plugin order changes timing
- bash installs a `DEBUG` trap and prepends `__onibi_precmd` to existing `PROMPT_COMMAND`
- fish writes a conf.d file using `fish_preexec`/`fish_postexec`; filename order controls load order
