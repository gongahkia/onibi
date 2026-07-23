# Troubleshooting

## Pairing Shows Forbidden

If the phone shows `Forbidden owner cookie is missing` right after pairing, its
browser did not trust Onibi's local HTTPS certificate for that run.

Do this:

```bash
./bin/onibi start
```

Then on the phone:

1. iPhone/iPad: install the printed `onibi-local-ca.mobileconfig`, then enable
   full trust for the Onibi local CA.
2. Android: install the printed `onibi-local-ca.crt` as a CA certificate in
   system Security settings.
3. Stop and restart `./bin/onibi start`.
4. Scan the new QR.

Old pair URLs expire and should not be reused.

## TLS Unknown Certificate

Server logs like this mean the phone rejected the local cert:

```text
TLS handshake error ... remote error: tls: unknown certificate
```

Install and fully trust the matching printed CA file, restart Onibi, then scan a new QR.

## Phone Cannot Reach The Mac

Check:

```bash
./bin/onibi start
```

Use the printed IP URL first. If `.local` fails, keep using the IP URL.

Common causes:

- Mac and phone are not on the same network.
- Managed Wi-Fi blocks client-to-client traffic.
- VPN or firewall blocks inbound local HTTPS.
- The QR was generated before switching networks.

Current fallback: connect the Mac to a phone hotspot, rerun `./bin/onibi start`, scan the new QR.

## WebSocket Token Mismatch

Logs like `reason=token_mismatch` usually mean a stale tab, stale token, or reload during reconnect.

Try:

1. Refresh the phone page once.
2. If it repeats, stop `onibi start`.
3. Run `./bin/onibi start` again.
4. Scan the new QR.

## Claude Shows Native Approval Instead Of Onibi Card

Check hooks:

```bash
./bin/onibi agent install --agent claude
./bin/onibi agent inspect --agent claude
```

Then in Claude Code, open `/hooks` and keep the Onibi hook commands enabled if they match.

Start Claude from the managed session created by `./bin/onibi start`, whether you are viewing it on the phone or after tapping `MAC`. That shell exports:

```bash
echo "$ONIBI_SOCK"
echo "$ONIBI_SESSION_ID"
```

If either is empty, the hook cannot route approval requests to the current web cockpit.

## Handover Does Not Open On macOS

Tap `MAC` again and check the toast. If it fails, run:

```bash
./bin/onibi show
```

With one active managed session, `show` targets it by default. If Ghostty is missing or launching fails, the error includes a `tmux attach-session` command; run it manually in Ghostty.

## Handover Back To Phone Reconnects

`PHONE` detaches the visible tmux clients and creates a fresh web attach host. A short reconnect toast is expected. The same cwd/history/running process should remain visible after reconnect.

## Approval Edit Fails

The edited payload must remain valid JSON.

For Claude Bash, edit the command object:

```json
{ "command": "touch /tmp/onibi-edited && echo edited" }
```

For file writes, preserve the expected fields such as `file_path` and `content`.

## Soft Keys

- `ESC` sends Escape for vim.
- `UP` and `DN` send arrow keys.
- `MAC` opens the current managed session in a visible macOS terminal.
- `PHONE` returns the current managed session to Safari.
- `INT` sends SIGINT to the running session.
- `KILL` terminates the hosted process.

If `ESC` or arrows seem ignored, tap the terminal once to focus it and try again.

## iOS Safari Backgrounding

Onibi sizes the cockpit with `100dvh` plus `visualViewport` resize/orientation events. When Safari or a PWA returns to the foreground, the page asks both WebSockets to reconnect immediately if iOS suspended them, then reattaches the PTY stream from the last snapshot sequence.

If the terminal looks stale after returning from the app switcher:

1. Wait for the `Resuming session...` marker to clear.
2. Rotate once, then rotate back.
3. If input is still frozen, reload the paired URL.
4. If reload fails, rerun `./bin/onibi start` and scan a fresh QR.

## Hook Drift

If hooks are missing or stale:

```bash
./bin/onibi agent status
./bin/onibi agent inspect --all
./bin/onibi agent install --agent claude
```

Agent-specific trust or reload prompts printed by `install-hooks` are required for that adapter.

## Clean Reset

Stop `onibi start`, then remove local state:

```bash
# macOS
rm -rf ~/Library/Application\ Support/onibi/

# Linux
rm -rf ~/.local/share/onibi/
```

Run `./bin/onibi start` and pair again.
