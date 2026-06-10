# Install Onibi on Raspberry Pi

Use a Raspberry Pi 5 with 64-bit Pi OS.

```sh
curl -fsSL https://raw.githubusercontent.com/gongahkia/onibi/v1.5.0/packaging/pi-install.sh | bash
onibi setup
onibi status
```

The Pi installer delegates to the Linux installer after checking for an `aarch64` userland. It installs the headless binary and enables the same user-level systemd service:

```ini
ExecStart=/usr/local/bin/onibi --headless --auto-transports
```

No USB or udev setup is required for v1.5. Onibi stores its database, bearer token fallback, VAPID keys, and LAN certificate under `~/.config/onibi`.

Troubleshooting:

```sh
systemctl --user status onibi.service
journalctl --user -u onibi.service -f
onibi token show
```
