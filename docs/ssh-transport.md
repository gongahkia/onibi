# SSH transport

`onibi up --ssh user@host[:port]` bootstraps a remote host over SSH, starts Onibi as that remote user, and keeps a local loopback tunnel open for phone pairing.

Use this when the terminal should run on a Raspberry Pi or another SSH host, but the paired phone should still connect to a local URL printed by the laptop.

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
  | 4. SFTP upload, chmod 0755
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
onibi up --ssh pi@raspberrypi.local --ssh-key ~/.ssh/id_ed25519
```

If `--ssh-key` is omitted, Onibi tries `~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`, then `~/.ssh/id_rsa`.

## Known-hosts policy

Onibi uses `~/.ssh/known_hosts` by default.

- Known matching host keys are accepted.
- Unknown host keys print the key type and SHA256 fingerprint and require typing `yes`.
- Host-key mismatches fail instead of prompting.
- Onibi does not use an insecure host-key callback.

For first contact, verify the fingerprint out of band before accepting it. For rebuilt Pis, remove the stale key with `ssh-keygen -R <host>` only after confirming the host was intentionally reprovisioned.

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

## Tunnel and pairing

The remote service uses `lan-loopback` so generated pair URLs target loopback instead of the remote LAN address. The local process opens an SSH TCP tunnel from an ephemeral local port to remote `127.0.0.1:8443`, then asks the remote binary to mint a pair URL for the local tunnel endpoint.

Ctrl-C in the local `onibi up --ssh ...` command closes the local tunnel and SSH client. It does not uninstall the remote service.

## Status and teardown

Check the remote service:

```bash
onibi ssh status pi@raspberrypi.local
```

Remove the remote service and installed binaries:

```bash
onibi ssh teardown pi@raspberrypi.local
```

Teardown stops and disables the user service, removes the unit or plist, and removes:

```text
$HOME/.local/bin/onibi
$HOME/.local/bin/onibi-notify
```

It does not delete Onibi state, certificates, logs, shell history, or tmux state from the remote home directory.

## Raspberry Pi memory notes

The active target budget is idle RSS under 80 MB on Raspberry Pi 5 4 GB. The physical Pi smoke is tracked separately in T2409; this page does not claim that result.

Operational checks:

```bash
pid=$(systemctl --user show -p MainPID --value onibi)
ps -o pid,rss,comm -p "$pid"
```

Memory tips:

- Prefer a 64-bit Pi OS on Pi 5 so the `linux/arm64` artifact is selected.
- Keep swap or zram configured at the OS level if agents, builds, or language servers run beside Onibi.
- Stop unused remote sessions before measuring idle RSS.
- Measure after the phone has disconnected and the service is idle.
- Use `onibi ssh teardown ...` before reprovisioning or switching artifacts.

Current local size check: `scripts/rpi-smoke.sh --size-only` builds the tagged Pi resource binary and gates it under the 14 MiB ceiling. Physical RSS validation still requires a Raspberry Pi target via `scripts/rpi-smoke.sh --target pi@raspberrypi.local`; target mode also sends SIGINT and fails if new `onibi-*` tmux sessions remain.
