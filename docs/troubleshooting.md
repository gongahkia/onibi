# Troubleshooting

## Lost Bot Token

Run:

```sh
onibi rotate-token
onibi doctor
```

Use @BotFather -> `/mybots` -> select bot -> API Token -> Revoke current token. Paste only the new token into Onibi.

## Lost Owner Telegram Account

Run:

```sh
onibi setup --rotate-owner
onibi doctor
```

This requires current owner confirmation, then pairs the new owner.

## Stale Webhook

Onibi uses long polling, not webhooks. Startup deletes any webhook. If updates still do not arrive:

```sh
onibi doctor
onibi rotate-token
onibi install-service
```

## No Telegram Updates

Check:

```sh
onibi doctor
onibi install-service
onibi log -n 20
```

Common causes: wrong bot token, another poller consuming updates, blocked Telegram network, service not running, or owner chat mismatch.

If doctor reports `telegram getUpdates` as `conflict: another getUpdates poller
is active`, Onibi has stopped its Telegram poll loop. Stop the other Onibi
daemon/process first. If you cannot identify it, revoke the token in @BotFather
and run:

```sh
onibi rotate-token
onibi install-service
onibi doctor --mode installed
```

## Sleep/Wake

If the laptop slept and messages stopped:

```sh
onibi doctor --fix
onibi install-service
```

Then send `/status` in Telegram. If it still fails, stop and restart the user service.

## Encrypted Approval Mode

Run:

```sh
onibi setup --enable-encrypted-mode
onibi config get telegram.encrypted_mode
onibi config get telegram.mini_app_url
```

If the Mini App says `seed missing`, scan the setup QR again. If Telegram opens
the app but actions do not return to the bot, verify the approval was opened
from the one-time keyboard button, not a normal browser.
If it says `SecureStorage unavailable`, update Telegram or use a Telegram client
that supports Mini App SecureStorage; Onibi does not fall back to browser storage.

## MCP

Use `onibi mcp` as a stdio server command in the MCP client config. The daemon
must already be running for session_input, session_peek, notify, and
approval_request; session_list can read the local DB without a live daemon.

## Hook Drift

If `onibi adapters` or `onibi doctor` reports hook drift:

```sh
onibi adapters
onibi hooks show --all
onibi doctor --fix
onibi install-hooks --interactive
```

`doctor --fix` adopts recognized current Onibi hooks with missing hashes and reinstalls outdated managed hooks. Tampered hooks require manual review.

Before or after installing one provider, inspect the exact file, backup, expected commands, installed commands, and trust/reload next step:

```sh
onibi hooks show --agent codex
onibi hooks show --agent claude
onibi hooks show --agent gemini
onibi hooks show --shell zsh
onibi doctor --after-upgrade
```

Provider-specific trust or reload prompts printed by `install-hooks` are required for that provider. Codex and Claude require manual review; OpenCode, Amp, and Pi require restart/reload behavior from their own CLI.

## Shell Hook Conflicts

Shell hooks are opt-in and emit `cmd_done` only after commands exceed the configured minimum duration. If events are missing, first run a command longer than the threshold, then check:

```sh
onibi adapters
onibi doctor
```

Common conflict points:

- Starship: Onibi does not replace the prompt; zsh uses `add-zsh-hook` for `preexec` and `precmd`. Keep both init blocks loaded, and reinstall Onibi hooks after prompt/framework setup if `cmd_done` events disappear.
- oh-my-zsh: load oh-my-zsh and plugins before the Onibi managed block. If a later plugin resets zsh hooks, move the managed block to the end by running `onibi install-hooks --shell zsh`.
- fish `conf.d`: Onibi owns `~/.config/fish/conf.d/onibi.fish` and rewrites it on install. Do not edit that file in place; put custom fish config in a separate `*.fish` file.
- bash plus `nix-shell --pure`: pure shells clear most environment variables, and Nix `shellHook` can change interactive state. If Bash notifications vanish inside Nix, source `~/.bashrc` after entering the shell or add the Onibi install/source step after the Nix shell setup.

## Keychain Fallback

If doctor reports `.env fallback`, the token is in the state dir instead of the OS keychain. Keep the file `0600`, install keychain support if needed, then rotate the token:

```sh
onibi rotate-token
onibi doctor
```

## Service Not Running

Run:

```sh
onibi install-service
onibi doctor --mode installed
```

macOS writes `~/Library/LaunchAgents/sh.onibi.daemon.plist`. Linux writes `~/.config/systemd/user/onibi.service`.
