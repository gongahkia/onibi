# Troubleshooting

## Pairing Shows Forbidden

If the phone shows `Forbidden owner cookie is missing` right after pairing, iOS did not trust Onibi's local HTTPS certificate for that run.

Do this:

```bash
./bin/onibi up
```

Then on iPhone:

1. Install the printed `onibi-local-ca.mobileconfig`.
2. Enable full trust for the Onibi local CA.
3. Stop and restart `./bin/onibi up`.
4. Scan the new QR.

Old pair URLs expire and should not be reused.

## TLS Unknown Certificate

Server logs like this mean the phone rejected the local cert:

```text
TLS handshake error ... remote error: tls: unknown certificate
```

Install and fully trust the printed CA profile, restart Onibi, then scan a new QR.

## Phone Cannot Reach The Mac

Check:

```bash
./bin/onibi up
```

Use the printed IP URL first. If `.local` fails, keep using the IP URL.

Common causes:

- Mac and phone are not on the same network.
- Managed Wi-Fi blocks client-to-client traffic.
- VPN or firewall blocks inbound local HTTPS.
- The QR was generated before switching networks.

Current fallback: connect the Mac to the iPhone hotspot, rerun `./bin/onibi up`, scan the new QR.

## WebSocket Token Mismatch

Logs like `reason=token_mismatch` usually mean a stale tab, stale token, or reload during reconnect.

Try:

1. Refresh the phone page once.
2. If it repeats, stop `onibi up`.
3. Run `./bin/onibi up` again.
4. Scan the new QR.

## Claude Shows Native Approval Instead Of Onibi Card

Check hooks:

```bash
./bin/onibi install-hooks --agent claude
./bin/onibi hooks --show --agent claude
```

Then in Claude Code, open `/hooks` and keep the Onibi hook commands enabled if they match.

Start Claude from the shell created by `./bin/onibi up`. That shell exports:

```bash
echo "$ONIBI_SOCK"
echo "$ONIBI_SESSION_ID"
```

If either is empty, the hook cannot route approval requests to the current web cockpit.

## Approval Edit Fails

The edited payload must remain valid JSON.

For Claude Bash, edit the command object:

```json
{"command":"touch /tmp/onibi-edited && echo edited"}
```

For file writes, preserve the expected fields such as `file_path` and `content`.

## Soft Keys

- `ESC` sends Escape for vim.
- `UP` and `DN` send arrow keys.
- `INT` sends SIGINT to the running PTY process group.
- `KILL` terminates the hosted process.

If `ESC` or arrows seem ignored, tap the terminal once to focus it and try again.

## Hook Drift

If hooks are missing or stale:

```bash
./bin/onibi adapters
./bin/onibi hooks --show --all
./bin/onibi install-hooks --agent claude
```

Provider-specific trust or reload prompts printed by `install-hooks` are required for that provider.

## MCP

Use `onibi mcp` as a stdio server command in the MCP client config. The daemon should already be running for daemon-backed tools.

## Clean Reset

Stop `onibi up`, then remove local state:

```bash
# macOS
rm -rf ~/Library/Application\ Support/onibi/

# Linux
rm -rf ~/.local/share/onibi/
```

Run `./bin/onibi up` and pair again.
