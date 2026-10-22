# Ghostty web control

Onibi's web interface controls the Ghostty application already running in your Mac's graphical login session. It does not move processes into a new terminal, add hooks to running coding agents, or keep the Mac awake. The Mac, Ghostty, and the Onibi web process must remain available.

## Private connection

Install Tailscale on both devices and sign into your private network. On the Mac:

```sh
tailscale serve --bg 8787
```

Use the HTTPS origin printed by Serve, without a path, as Onibi's public origin:

```sh
./bin/onibi web --origin https://your-mac.your-tailnet.ts.net
```

Onibi binds `127.0.0.1:8787`; `--listen` accepts only loopback addresses. Serve terminates HTTPS and proxies to the local server. Follow Tailscale's HTTPS consent flow if prompted. Tailnet access rules still apply. See the [official Serve setup](https://tailscale.com/docs/features/tailscale-serve) and [CLI reference](https://tailscale.com/docs/reference/tailscale-cli/serve).

The printed link contains a one-use pairing secret in its URL fragment. Open it on the intended iPhone browser within ten minutes. The page removes the fragment immediately, exchanges the secret for an HttpOnly session cookie, and keeps its CSRF token only in memory. Terminal text and pairing secrets are not stored in browser local storage. A manual pairing field is available when copying the link is inconvenient.

For local development, omit `--origin` and use the printed loopback HTTP link on the Mac. For phone access, use the configured HTTPS origin consistently; switching between the loopback and private addresses does not share a browser session.

## Controls and output

- Choose a pane explicitly from the live window/tab list. Input never falls back to whichever terminal happens to be in front.
- Send appends Enter. Paste sends the exact text without Enter. Keyboard shortcuts are explicit actions.
- **Focus & read** brings the chosen pane forward on the Mac. Output capture reads macOS Accessibility text for the focused pane only after verifying its native identity. If identity, permissions, or output access cannot be verified, the interface reports the problem.
- Output is bounded visible plaintext. It is not a pixel mirror, full scrollback, or a terminal emulator. ANSI colors, images, and interactive mouse applications are outside this first interface.
- Create a tab in an explicitly selected existing window. Close a tab only after confirming its name; all panes in that tab are affected.

Ghostty's [AppleScript API](https://ghostty.org/docs/features/applescript) controls native terminals by identity. Stock Ghostty 1.3 exposes visible text through Accessibility, but not through a UUID-addressable AppleScript text property. Onibi therefore avoids guessing a pane from its title or screen position. See the [Ghostty Accessibility implementation](https://github.com/ghostty-org/ghostty/blob/v1.3.1/macos/Sources/Ghostty/Surface%20View/SurfaceView_AppKit.swift).

macOS may require Automation permission to control Ghostty and Accessibility permission to read terminal text. Use System Settings → Privacy & Security for the application or process macOS identifies as launching Onibi. Capture requires a native macOS build with CGO enabled; a CGO-disabled build reports capture unavailable while retaining supported controls.

## Delivery and recovery

Each action has a unique receipt recorded before execution. Reusing the same ID and payload returns its prior state; changing the payload under an existing ID is rejected. Network loss causes the browser to check the receipt, never silently resend the input. An action interrupted by a crash or an ambiguous native error is marked uncertain. Inspect the terminal before deciding whether to send a new action.

Receipts retain hashes and decision metadata, not command text. The browser keeps only an unresolved receipt's ID, action kind and destination IDs in tab-scoped session storage so a page reload can check its outcome; drafts and credentials are never stored there. A server restart revokes browser sessions and marks unfinished receipts uncertain. Only one web process can use a state directory at a time. Logout revokes that browser's session. Stop `onibi web` to cut off browser control; in-flight actions may finish during shutdown, and existing Ghostty terminals continue running.

`onibi system service install` currently installs the Telegram daemon, not the web process. Run the web command in a foreground session for this first version. Tailscale Serve's background setting keeps the proxy running, but does not start Onibi after a reboot.

## Verification boundaries

Hermetic backend tests cover native command construction, targeting failures, HTTP authentication, origin/CSRF enforcement, and action recovery. Browser fixtures exercise phone layouts and control states. Those checks do not establish an actual iPhone/Tailscale connection or permission behavior on every macOS/Ghostty combination; perform a harmless command in a disposable tab when pairing your own phone.

The initial native smoke on Ghostty 1.3.1 verified inventory identities, but Ghostty then stopped answering automation requests during focus. Input and output capture were not reached, and cleanup also timed out. This is a preview with native end-to-end validation still outstanding; automated success alone does not establish that the local Ghostty installation responds to every control. Onibi reports interrupted actions as uncertain and does not restart Ghostty or replay them.

Run browser regressions independently with `cd web-tests && npm ci && npx playwright install chromium && npm test`. The Node dependencies are test-only; the application remains a single Go binary with embedded assets.
