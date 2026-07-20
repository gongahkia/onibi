# systemd relay package

Build and validate a native Debian package on a Linux host with `dpkg-deb` and `systemd-analyze`:

```text
deploy/relay/systemd/package-test.sh
```

The package contains `yeokcham-relay`, `/etc/yeokcham/relay.conf` as a preserved Debian conffile, and `yeokcham-relay.service`. It supports only 64-bit GNU Linux Rust targets and refuses relative, non-`.deb`, or existing output paths.

For a macOS build host, validate in Debian without writing build artifacts into the checkout:

```text
docker run --rm -e CARGO_TARGET_DIR=/tmp/target -v "$PWD:/workspace" -w /workspace rust:1.93.0-bookworm sh -ceu 'apt-get update; apt-get install -y --no-install-recommends systemd; deploy/relay/systemd/package-test.sh'
```

Install and configure a built package:

```text
sudo dpkg -i /absolute/path/yeokcham-relay.deb
sudoedit /etc/yeokcham/relay.conf
sudo /usr/bin/yeokcham-relay generate-identity --output /etc/yeokcham/relay.identity
sudo systemctl daemon-reload
sudo systemctl enable --now yeokcham-relay.service
```

The service requires systemd 247 or newer, runs with `DynamicUser=`, gives the relay a writable `/var/lib/yeokcham` state directory, and passes the root-owned, read-only identity through `LoadCredential=`. It does not auto-start on installation. Inspect it with `systemctl status yeokcham-relay.service`, `journalctl -u yeokcham-relay.service`, and `systemd-analyze security yeokcham-relay.service`.
