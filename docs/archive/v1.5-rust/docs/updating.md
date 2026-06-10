# Updating Onibi

## Desktop app

The desktop app checks GitHub Releases for signed updates. It prompts before
installing and relaunching.

Manual check:

```sh
Command Palette -> Check for Updates
```

The same action is available under Settings -> Updates.

## Headless installs

Linux and Pi headless installs use the CLI updater:

```sh
onibi update check
onibi update install
```

Use JSON output for automation:

```sh
onibi --json update check
onibi --json update install --yes
```

The installer downloads the matching headless binary from GitHub Releases,
verifies the SHA256 checksum, verifies the release signature when the public key
is compiled into the binary, replaces the current executable, and restarts the
user `onibi.service` when it is active or enabled.

## Release signing

GitHub Actions expects these release secrets or variables:

| Name | Purpose |
| --- | --- |
| `TAURI_UPDATER_PUBLIC_KEY` | Public Tauri updater key embedded into `tauri.conf.json` at release build time. |
| `TAURI_SIGNING_PRIVATE_KEY` | Private Tauri updater key used by `tauri build` to sign GUI updater artifacts. |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | Password for the Tauri signing key. Empty is allowed if the key was generated without a password. |
| `HEADLESS_UPDATE_PUBLIC_KEY` | Base64-encoded DER P-256 public key compiled into headless release binaries. |
| `HEADLESS_UPDATE_SIGNING_KEY` | PEM P-256 private key used to sign headless release binaries. |

Generate Tauri updater keys:

```sh
pnpm --filter onibi-app tauri signer generate -- -w ~/.tauri/onibi-updater.key
```

Generate headless updater keys:

```sh
openssl ecparam -name prime256v1 -genkey -noout -out headless-update.key
openssl ec -in headless-update.key -pubout -outform DER | base64 | tr -d '\n'
```

Store the PEM private key as `HEADLESS_UPDATE_SIGNING_KEY` and the printed
base64 public key as `HEADLESS_UPDATE_PUBLIC_KEY`.

Release tags publish:

- `latest.json` for Tauri GUI updates.
- `latest-headless.json` for CLI/headless updates.
