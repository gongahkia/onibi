# SSH transport

`onibi up --ssh user@host[:port]` bootstraps a remote host over SSH, starts Onibi as that remote user, and keeps a local loopback tunnel open for phone pairing.

Use this when the terminal should run on an SSH host, but the paired phone should still connect to a local URL printed by the laptop.

## Bootstrap flow

```text
local onibi
  |
  | 1. SSH dial with private-key auth
  v
remote host
  |
  | 2. uname -sm
  v
local artifact picker
  |
  | 3. select matching onibi + onibi-notify binaries
  v
remote /tmp/onibi.<token>.<arch>
  |
  | 4. SFTP upload, chmod 0755, SHA-256 verification
  v
remote $HOME/.local/bin
  |
  | 5. atomic mv into onibi + onibi-notify
  v
remote user service
  |
  | 6. start onibi up --transport=lan-loopback --no-qr
  v
local SSH tunnel
  |
  | 7. 127.0.0.1:<local-port> -> remote 127.0.0.1:8443
  v
remote pair URL minted through onibi pair
```

Example:

```bash
onibi up --ssh user@example.internal --ssh-key ~/.ssh/id_ed25519
```

If `--ssh-key` is omitted, Onibi tries `~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`, then `~/.ssh/id_rsa`.

## Known-hosts policy

Onibi uses `~/.ssh/known_hosts` by default.

- Known matching host keys are accepted.
- Unknown host keys print the key type and SHA256 fingerprint and require typing `yes`.
- Host-key mismatches fail instead of prompting or replacing the pin. The error identifies the presented fingerprint and requires explicit reprovisioning confirmation before updating `known_hosts`.
- Onibi does not use an insecure host-key callback.

For first contact, verify the fingerprint out of band before accepting it. For rebuilt hosts, remove the stale key with `ssh-keygen -R <host>` only after confirming the host was intentionally reprovisioned.

## Service model

Linux installs a user systemd unit:

```text
~/.config/systemd/user/onibi.service
```

The unit runs:

```text
%h/.local/bin/onibi up --transport=lan-loopback --no-qr
```

macOS installs a LaunchAgent:

```text
~/Library/LaunchAgents/io.onibi.plist
```

Onibi uses user services instead of root services because the remote terminal, config, certificates, SQLite state, and installed binaries all live in the target user's home directory. Running as root would cross UID boundaries, require privileged install paths, and make terminal sessions less representative of the user shell the phone is meant to control.

Bootstrap verifies the SHA-256 of each uploaded binary on the remote host before replacing either executable. Reinstalling reloads and restarts the user service, so a failed replacement remains explicit instead of leaving an old daemon running unnoticed.

## Tunnel and pairing

The remote service uses `lan-loopback` so generated pair URLs target loopback instead of the remote LAN address. The local process opens an SSH TCP tunnel from an ephemeral local port to remote `127.0.0.1:8443`, then asks the remote binary to mint a pair URL for the local tunnel endpoint.

Ctrl-C in the local `onibi up --ssh ...` command closes the local tunnel and SSH client. It does not uninstall the remote service.

If a forwarded connection fails, the local listener stays open and retries that forward once through a newly authenticated SSH client using the existing host-key pin. Reconnection never reinstalls or restarts the remote daemon.

## Status and teardown

Check the remote service:

```bash
onibi ssh status user@example.internal
```

Remove the remote service and installed binaries:

```bash
onibi ssh teardown user@example.internal
```

Teardown stops and disables the user service, removes the unit or plist, and removes:

```text
$HOME/.local/bin/onibi
$HOME/.local/bin/onibi-notify
```

It does not delete Onibi state, certificates, logs, shell history, or tmux state from the remote home directory.

## Remote resource notes

The default remote smoke budget is a stripped `linux/arm64` binary under 14 MiB and idle RSS under 80 MiB. Those limits are intentionally conservative for small always-on SSH hosts; larger servers may raise them with `ONIBI_SSH_MAX_BINARY_BYTES` and `ONIBI_SSH_MAX_RSS_KIB`.

Operational checks:

```bash
pid=$(systemctl --user show -p MainPID --value onibi)
ps -o pid,rss,comm -p "$pid"
```

Memory tips:

- Prefer a 64-bit Linux OS on arm64 hosts so the `linux/arm64` artifact is selected.
- Keep swap or zram configured at the OS level if agents, builds, or language servers run beside Onibi.
- Stop unused remote sessions before measuring idle RSS.
- Measure after the phone has disconnected and the service is idle.
- Use `onibi ssh teardown ...` before reprovisioning or switching artifacts.

Current local size check: `scripts/ssh-smoke.sh --size-only` builds the tagged remote SSH binary and gates it under the 14 MiB ceiling. RSS validation still requires a real SSH target via `scripts/ssh-smoke.sh --target user@example.internal`; target mode also sends SIGINT and fails if new `onibi-*` tmux sessions remain.
