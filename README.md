<p align="center">
  <img src="internal/web/assets/icon.svg" alt="Onibi" width="96">
</p>

# Onibi

<p align="center">
  <a href="docs/web.md">Phone cockpit</a> | <a href="docs/managed-sessions.md">Persistent sessions</a> | <a href="CHANGELOG.md">Changelog</a>
</p>
<p align="center">
  <a href="https://github.com/gongahkia/onibi/releases/latest"><img src="https://img.shields.io/github/v/release/gongahkia/onibi?style=for-the-badge&color=FFB000" alt="Latest release"></a>
  <a href="https://github.com/gongahkia/onibi/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/gongahkia/onibi/ci.yml?branch=main&style=for-the-badge&label=CI" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache--2.0-green?style=for-the-badge" alt="License: Apache 2.0"></a>
  <img src="https://img.shields.io/badge/macOS-Ghostty-black?style=for-the-badge" alt="macOS and Ghostty">
</p>

**Your Mac's terminals, in your pocket.** Onibi runs on your Mac and lets a paired phone read and drive the terminals you already have open: Ghostty windows, tabs and panes, and persistent tmux, Zellij or Screen sessions that keep running after Ghostty quits. Start a coding agent at your desk, then watch it, answer it and get notified when it needs you from anywhere on your private network.

Run any program you like (`codex`, `claude`, `pi`, a shell, a build). Onibi gives none of them special treatment: it reads the screen, sends the keys a person would press, and watches for the generic signs that a program is waiting on you.

<table>
<tr><td><b>Your real terminals</b></td><td>Select an exact Ghostty pane, read its visible text, send literal input or keys, split panes, open and close tabs and windows. Closing anything asks for a confirmation that names it.</td></tr>
<tr><td><b>Sessions that outlive Ghostty</b></td><td><code>onibi session new</code> starts a program in tmux, Zellij or Screen. The phone reads scrollback and colours without focusing anything, and the session keeps working when Ghostty is closed or the phone disconnects.</td></tr>
<tr><td><b>Told when it needs you</b></td><td>Generic attention signals (needs input, bell, active, idle) show as badges and in the tab title. Opt-in push notifications name the session and the reason, never its output.</td></tr>
<tr><td><b>One-tap replies</b></td><td>When a program asks a yes/no question or shows a numbered menu, the phone offers the same key press a person would make. Nothing is ever sent automatically.</td></tr>
<tr><td><b>Private by construction</b></td><td>The server listens on loopback only. Tailscale Serve provides private HTTPS. Pairing links are single-use, sessions are HttpOnly cookies, and every action has a durable receipt, so a lost connection never repeats a command.</td></tr>
<tr><td><b>No hosted relay</b></td><td>There is no Onibi cloud. Terminal text never leaves your Mac except over your own Tailscale connection. Terminal output is never put in a push notification.</td></tr>
</table>

---

## Quick Install

You need a Mac with [Ghostty](https://ghostty.org) 1.3 or later for Ghostty panes, and [Tailscale](https://tailscale.com) on the Mac and the phone. Persistent sessions need one of `tmux`, Zellij or GNU Screen.

### From a release

Download the archive for your Mac from the [latest release](https://github.com/gongahkia/onibi/releases/latest), check it against `checksums.txt`, and put `onibi` on your `PATH`:

```sh
gh release download -R gongahkia/onibi -p 'onibi_*_darwin_arm64.tar.gz' -p checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar xzf onibi_*_darwin_arm64.tar.gz
install -m 755 onibi /usr/local/bin/onibi
onibi --version
```

Use `darwin_amd64` on an Intel Mac. Linux archives are published for the persistent-sessions daemon; the Ghostty cockpit is macOS only.

### From source

```sh
git clone https://github.com/gongahkia/onibi && cd onibi
make build        # needs the Go version in go.mod and Xcode Command Line Tools
./bin/onibi web
```

`make install` copies the binary into place and `make update` upgrades it. `make help` lists every target.

---

## Getting Started

1. Expose the local server over private HTTPS on your tailnet:

   ```sh
   tailscale serve --bg 8787
   ```

2. Start Onibi with the address Tailscale prints:

   ```sh
   onibi web --origin https://your-mac.your-tailnet.ts.net
   ```

3. Open the pairing link it prints on your phone within ten minutes. It works once. Name the browser, choose whether to remember it for 30 days, and connect.
4. Allow macOS Automation and Accessibility access for the app that launched Onibi when asked, then run `onibi doctor` if anything looks off.

For a quick look without a phone, run `onibi web` and open the printed link in a private window on the Mac.

To keep it running in the background, install the login service:

```sh
onibi web service install --origin https://your-mac.your-tailnet.ts.net
onibi web service start
onibi web pair            # print a fresh pairing link
```

On iPhone, add Onibi to your Home Screen (Share, then Add to Home Screen) to turn on notifications.

---

## CLI Quick Reference

| Command | What it does |
| --- | --- |
| `onibi web` | Start the phone cockpit and print a pairing link |
| `onibi web pair` | Print a fresh one-use link without restarting |
| `onibi web devices` | List or revoke paired browsers |
| `onibi web service install\|start\|stop\|status\|remove` | Run the cockpit as a login service |
| `onibi session new [--name] [--cwd] <program> [args...]` | Run a program in a persistent session |
| `onibi session list` | List sessions with live attention |
| `onibi session attach <name\|id>` | Attach this terminal to a session |
| `onibi session rename <name\|id> <new-name>` | Rename a session |
| `onibi session kill <name\|id>` | End a session after confirmation |
| `onibi session adopt <tmux\|zellij\|screen> <target>` | Take over an existing multiplexer session |
| `onibi doctor` | Check that everything is ready |
| `onibi web logs` | Read private web telemetry |

From the phone, **New session** starts a program in a folder of your choice, and arguments follow shell quoting (`codex "fix the build"`) without any expansion.

---

## Documentation

- [Phone cockpit](docs/web.md): setup, Tailscale, pairing, readiness, controls, recovery, background service and verification limits
- [Persistent sessions](docs/managed-sessions.md): the daemon, attention signals, CLI and what is no longer supported
- [Changelog](CHANGELOG.md)
- [Security policy](SECURITY.md) and [threat model](THREAT-MODEL.md)
- [Product audit](docs/product-audit.md), [cockpit review](docs/cockpit-review.md) and [readiness review](docs/readiness-review.md)

### Honest limits

- Ghostty panes can be read only while focused, and they never notify: Ghostty exposes no bell or output events to Onibi. Use a persistent session when you want to be told.
- Onibi does not keep your Mac awake. The Mac, a graphical login, Ghostty and the Onibi process must stay available.
- There is no browser terminal emulator, no hosted relay and no team sharing.
- Physical-iPhone and live macOS permission behaviour are only partly validated; see [verification boundaries](docs/web.md#verification-boundaries).

---

## Contributing

```sh
make test            # race-enabled Go tests
make test-browser    # Chromium and WebKit tests against the real server
```

Run `go vet ./...` before sending a change. CI runs the same checks on macOS and Linux, plus the browser suites and a release snapshot. Report security problems as described in [SECURITY.md](SECURITY.md).

---

## License

Apache 2.0, see [LICENSE](LICENSE). Third-party notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
