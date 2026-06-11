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

## MCP

Use `onibi mcp` as a stdio server command in the MCP client config. The daemon
must already be running for session_input, session_peek, notify, and
approval_request; session_list can read the local DB without a live daemon.

## Hook Drift

If `onibi adapters` or `onibi doctor` reports hook drift:

```sh
onibi adapters
onibi doctor --fix
onibi install-hooks --interactive
```

`doctor --fix` only adopts recognized current Onibi hooks with missing hashes. Tampered or outdated hooks require reinstall.

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
