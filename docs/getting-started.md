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
- A phone browser on the same reachable network as the Mac, or an iPhone hotspot.
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
./bin/onibi doctor
```

Install Claude hooks when testing Claude approvals:

```bash
./bin/onibi install-hooks --agent claude
./bin/onibi hooks --show --agent claude
```

For a new machine with multiple supported agents already configured, preview
and install detected hooks:

```bash
./bin/onibi install-hooks --dry-run
./bin/onibi install-hooks --all
```

Claude may require you to open `/hooks` and trust the printed Onibi hook commands.

## 3. Pair The Phone

Start Onibi:

```bash
./bin/onibi up
```

Onibi prints:

- A local CA profile path, usually `onibi-local-ca.mobileconfig`.
- A LAN or hotspot pair URL.
- A QR code for the pair URL.

On iPhone:

1. Install the printed CA profile if Safari warns about trust.
2. Enable full trust for the Onibi local CA in iOS certificate trust settings.
3. Restart `./bin/onibi up`.
4. Scan the new QR.

If pairing returns `Forbidden owner cookie is missing`, the browser did not complete trusted local HTTPS setup. Install and fully trust the profile, restart Onibi, then scan the new QR. Do not reuse an old QR.

Network rule:

- Same Wi-Fi works only when the network allows client-to-client traffic.
- Managed Wi-Fi may block pairing or WebSockets.
- iPhone hotspot is the current recommended fallback.

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
./bin/onibi up
./bin/onibi doctor
./bin/onibi doctor --fix
./bin/onibi doctor --release --offline
./bin/onibi adapters
./bin/onibi hooks --show --agent claude
./bin/onibi install-hooks --agent claude
```

## 7. Clean Reset

Stop `onibi up`, inspect the uninstall plan, then remove hooks/service/state:

```bash
./bin/onibi uninstall --dry-run --service --hooks --all-hooks --state
./bin/onibi uninstall --dry-run --json --service --hooks --all-hooks --state
./bin/onibi uninstall --yes --service --hooks --all-hooks --state
```

Then pair again:

```bash
./bin/onibi up
```
