# Getting Started

This guide takes a fresh machine from no Onibi state to one working LAN web cockpit session.

For release sign-off on a stock macOS user and stock Ubuntu VM, use
[`fresh-machine-smoke.md`](./fresh-machine-smoke.md). This guide is the shorter
source-build path.

## 1. Prerequisites

You need:

- macOS or Linux.
- Go 1.26.4 or newer when building from source.
- `git`, `make`, and a local shell.
- A phone browser on the same reachable network as the Mac, or a phone hotspot.
- Optional: Claude Code installed if you want approval overlay tests.

Onibi stores local state in:

```bash
# macOS
~/Library/Application\ Support/onibi/

# Linux
~/.local/share/onibi/
```

The phone cockpit is served over local HTTPS. The daemon also opens a same-UID Unix socket for approval hooks.

## 2. Install

Build from source:

```bash
git clone https://github.com/gongahkia/onibi
cd onibi
make build
```

Confirm the binary works:

```bash
./bin/onibi version
./bin/onibi system doctor
```

Install Claude hooks when testing Claude approvals:

```bash
./bin/onibi agent install --agent claude
./bin/onibi agent inspect --agent claude
```

For a new machine with multiple supported agents already configured, preview
and install detected hooks:

```bash
./bin/onibi agent install --dry-run
./bin/onibi agent install --all
```

Claude may require you to open `/hooks` and trust the printed Onibi hook commands.

## 3. Pair The Phone

`onibi system doctor` gives the next safe action before you start. For local/private
transports, `onibi start` then prints the exact trust files for both phone
platforms and the fresh pairing QR.

Start Onibi:

```bash
./bin/onibi start
```

Onibi prints:

- An iPhone/iPad profile path, `onibi-local-ca.mobileconfig`.
- An Android CA certificate path, `onibi-local-ca.crt`.
- A LAN or hotspot pair URL.
- A QR code for the pair URL.

For a local/private transport, use a transfer channel you control, such as
private file sync or a USB cable, to move only the matching trust file from the
Mac to the phone. Do not download a CA file from the local network.

1. iPhone/iPad: install `onibi-local-ca.mobileconfig`, then enable full trust
   for `Onibi local CA` in Certificate Trust Settings.
2. Android: install `onibi-local-ca.crt` as a CA certificate through system
   Security settings; the menu names vary by device.
3. Scan the QR only after the certificate is trusted.
4. Add the paired cockpit to Home Screen or install it as a web app.

If pairing returns `Forbidden owner cookie is missing`, the browser did not complete trusted local HTTPS setup. Install and fully trust the profile, restart Onibi, then scan the new QR. Do not reuse an old QR.

Network rule:

- Same Wi-Fi works only when the network allows client-to-client traffic.
- Managed Wi-Fi may block pairing or WebSockets.
- A phone hotspot is the current recommended fallback.

## 4. Use The Cockpit

After pairing, Safari opens the terminal cockpit.

Soft keys:

| key | sends |
|---|---|
| `Esc` | Escape, useful for vim |
| `Tab` | Tab |
| `Ctrl` | one-shot Ctrl modifier |
| `Alt` | one-shot Alt modifier |
| `↑` / `↓` / `←` / `→` | arrows; long-press repeats |
| `^C` / `^D` / `^Z` | Ctrl-C, Ctrl-D, Ctrl-Z |
| `Paste` | reads the iOS clipboard and sends it to the PTY |

iOS Safari shows a clipboard permission prompt the first time `Paste` reads the clipboard.
Image paste accepts PNG, JPEG, and WebP files up to 2MB, stores them under the local Onibi uploads directory, and inserts the local path into the terminal. SVG image paste is rejected.
The top `INT` control sends SIGINT to the hosted process; `KILL` terminates it.

Basic smoke:

```bash
vim /tmp/onibi-smoke.txt
```

Type text, tap `ESC`, then type `:wq`.

## 5. Test Claude Approvals

Start Claude from the Onibi cockpit shell:

```bash
claude
```

Ask Claude to create a temp file. When the approval card appears on the phone:

- `Approve` lets the original tool input run.
- `Deny` blocks it.
- `Edit` lets you submit modified JSON input.

While an approval card is pending, supported mobile browsers may request a screen
wake lock so the phone does not sleep before you decide. This can increase
battery use. To opt out in Safari or Chrome dev tools, run:

```js
localStorage.setItem("onibi:wake-lock", "off")
```

Example edit test:

1. Ask Claude to run `touch /tmp/onibi-original && echo original`.
2. Tap `Edit`.
3. Replace the Bash input with `{"command":"touch /tmp/onibi-edited && echo edited"}`.
4. Submit the edit.
5. Verify `/tmp/onibi-original` is absent and `/tmp/onibi-edited` exists.

## 6. Common Commands

```bash
./bin/onibi start
./bin/onibi system doctor
./bin/onibi system doctor --fix
./bin/onibi system doctor --release --offline
./bin/onibi agent adapter validate <path>
./bin/onibi agent inspect --agent claude
./bin/onibi agent install --agent claude
```

## 7. Clean Reset

Stop `onibi start`, inspect the uninstall plan, then remove hooks/service/state:

```bash
./bin/onibi system uninstall --dry-run --service --hooks --all-hooks --state
./bin/onibi system uninstall --dry-run --json --service --hooks --all-hooks --state
./bin/onibi system uninstall --yes --service --hooks --all-hooks --state
```

Then pair again:

```bash
./bin/onibi start
```
