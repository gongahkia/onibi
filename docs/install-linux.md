# Install Onibi on Linux

Onibi Linux hosts run the headless daemon as a user-level systemd service.

```sh
curl -fsSL https://raw.githubusercontent.com/gongahkia/onibi/v1.5.0/packaging/install-linux.sh | bash
onibi setup
onibi status
```

The installer downloads the release binary for `x86_64` or `aarch64`, installs it to `/usr/local/bin/onibi`, writes `~/.config/systemd/user/onibi.service`, then runs:

```sh
systemctl --user daemon-reload
systemctl --user enable --now onibi.service
```

Manual service control:

```sh
systemctl --user status onibi.service
systemctl --user restart onibi.service
journalctl --user -u onibi.service -f
```

If `systemctl --user` is unavailable in a minimal session, start the daemon manually:

```sh
onibi --headless --auto-transports
```

The daemon listens on `127.0.0.1:17893`. Pair from a phone with:

```sh
onibi setup
```
