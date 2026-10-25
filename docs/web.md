# Ghostty web control

Onibi's web interface controls the Ghostty application already running in your Mac's graphical login session. It does not move processes into a new terminal, add hooks to running coding agents, or keep the Mac awake. The Mac, Ghostty, and the Onibi web process must remain available.

The cockpit is served by the Mac over private Tailscale HTTPS. This repository does not use GitHub Pages: the former `/docs` publishing configuration was retired. Documentation stays in the repository and release archives; Pages is not part of deployment or CI verification.

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

## Mac readiness

Start with a passive check in the same launching environment as the web process:

```sh
./bin/onibi web doctor
./bin/onibi web doctor --json --origin https://your-mac.your-tailnet.ts.net
```

This inspects the Ghostty app bundle/version, running-process count, and the calling process's Accessibility authorization. It validates listener/origin syntax without binding a port or contacting Tailscale. It sends no Ghostty Apple events, requests no permission prompts, changes no focus, and reads no terminal text. Automation remains **unknown** until a native request is made. Unknown is not proof of readiness; a successful exit means no known failing check, not a verified end-to-end connection. Known failures return a nonzero exit code and still print the full JSON report when requested.

When ready to test native responsiveness, explicitly run `onibi web doctor --probe`. This sends one bounded inventory request and can prompt for Automation permission. Adding `--read-target '<exact pane ID>'` verifies focused-pane capture and discards the captured text; it never focuses the pane for you. Do not add these options when you only want passive diagnostics.

On the phone, tap the connection status to open **Mac readiness**. It shows passive permission/process details, the installed version, the last native inventory duration, and browser session expiry. **Check again** refreshes diagnostics and requests inventory when its retry delay permits. Observations describe the last completed request, not a guarantee about the next one.

| Phone status | Meaning and next action |
| --- | --- |
| Connected | The Mac is reachable and native inventory responded. Select a pane to read or control it. |
| Disconnected | HTTP transport to the Mac failed. Check the Mac, Onibi process, and Tailscale route. Page-memory drafts remain available. |
| Checking Ghostty | Inventory is being established or reconciled after a rejected action. |
| Ghostty unavailable | The Mac is reachable but native control could not be verified. The detail distinguishes not running, Automation permission, timeout, and communication failures. Checks back off automatically. |
| Focus needed | Input can still target the selected pane. Use the explicit **Focus & read** action to change the Mac's focus before reading. |
| Output unavailable | Native control is available but capture failed. Correct Accessibility permission or the reported issue, then use **Retry output**. |
| Update Ghostty | The known installed version lacks the required native controls. Titles require 1.3.1; other supported controls require 1.3.0. |

Healthy inventory polling waits ten seconds after the previous request settles while the page is visible. Failure backoff grows to sixty seconds. Concurrent server inventory polls share in-flight work, and abandoned reads are canceled when their last waiter leaves. **Every mutation still checks a fresh inventory**; polling backoff never substitutes cached targets for execution preconditions. A rejected action reconciles readiness immediately. Successful input/key delivery avoids an extra inventory request and debounces its output read by 400 ms, reducing repeated work during key entry.

## Controls and output

- Search by pane, tab, window, or working directory, then choose a pane explicitly from the live hierarchy. Input never falls back to whichever terminal happens to be in front.
- Send appends Enter. Paste sends the exact text without Enter. Keyboard shortcuts are explicit actions.
- **Focus & read** brings the chosen pane forward on the Mac. Output capture reads macOS Accessibility text for the focused pane only after verifying its native identity. If identity, permissions, or output access cannot be verified, the interface reports the problem.
- Output is bounded visible plaintext. It is not a pixel mirror, full scrollback, or a terminal emulator. ANSI colors, images, and interactive mouse applications are outside this first interface.
- Create a window even when Ghostty has no open panes; Ghostty must already be running. Create a tab in the selected window, or split the selected pane left, right, above, or below. New destinations use Ghostty's native defaults and are selected after their identities appear in inventory.
- **Controls** exposes pane zoom, balanced splits, 40-pixel split resizing, scrollback pages/top/bottom, previous/next prompt, Mac font size, fullscreen, and pane/tab titles. Prompt navigation needs Ghostty shell integration. These controls require the selected pane to be focused on the Mac; Onibi never focuses it implicitly. Known Ghostty 1.3.0 installations have title controls disabled in the phone UI and rejected before dispatch. Unknown/development versions remain explicitly unknown; a version alone does not prove permission or responsiveness.
- **More keys** offers Ctrl/Alt A–Z, arrows, Home/End, Page Up/Down, Shift Tab, Backspace/Delete, Space, and F1–F12. Each tap is a key action for the named pane. The target program and Ghostty configuration determine its meaning. Ctrl C interrupts, Ctrl D can exit a shell, and Ctrl Z can suspend a process.
- Close a pane, tab, or window only after checking the named confirmation and affected pane list. A tab/window close affects all its panes. The displayed list is a snapshot; another local user can change it before confirmation. A confirmed close request does not itself prove that Ghostty removed the destination—check the refreshed list.
- Drafts belong to their exact pane and stay only in page memory. Switching panes restores that pane's draft; a removed pane's draft never transfers to another shell. Network loss or session expiry preserves page-memory drafts. Reloading, closing the page, or choosing **Disconnect** discards them. Delivery recovery stores only an unresolved receipt and destination IDs.
- **Follow** requests fresh visible text 2.5 seconds after the previous request settles. Requests do not overlap. Switching panes stops Follow; hiding the page pauses it, and returning reads again. Capture errors or going offline stop it until you explicitly enable it again. Output age is visible, and the view preserves your scroll position unless you were already following the bottom. Follow cannot read an unfocused pane or keep your Mac awake.

Ghostty's [AppleScript API](https://ghostty.org/docs/features/applescript) controls native terminals by identity. Stock Ghostty 1.3 exposes visible text through Accessibility, but not through a UUID-addressable AppleScript text property. Onibi therefore avoids guessing a pane from its title or screen position. See the [Ghostty Accessibility implementation](https://github.com/ghostty-org/ghostty/blob/v1.3.1/macos/Sources/Ghostty/Surface%20View/SurfaceView_AppKit.swift).

macOS may require Automation permission to control Ghostty and Accessibility permission to read terminal text. Use System Settings → Privacy & Security for the application or process macOS identifies as launching Onibi. Capture requires a native macOS build with CGO enabled; a CGO-disabled build reports capture unavailable while retaining supported controls.

## Delivery and recovery

Each action has a unique receipt recorded before execution. Reusing the same ID and payload returns its prior state; changing the payload under an existing ID is rejected. Network loss causes the browser to check the receipt, never silently resend the input. An action interrupted by a crash or an ambiguous native error is marked uncertain. Inspect the terminal before deciding whether to send a new action.

Receipts retain hashes and decision metadata, not command text. The browser keeps only an unresolved receipt's ID, action kind and destination IDs in tab-scoped session storage so a page reload can check its outcome; drafts and credentials are never stored there. Browser sessions expire after twelve hours. A server restart revokes browser sessions and marks unfinished receipts uncertain. Only one web process can use a state directory at a time. Logout revokes that browser's session. Stop `onibi web` to cut off browser control; in-flight actions may finish during shutdown, and existing Ghostty terminals continue running. A fresh pairing link currently requires restarting the web process; this also revokes other browser sessions. Device enrollment and renewal policy remain future work.

`onibi system service install` currently installs the Telegram daemon, not the web process. Run the web command in a foreground session for this first version. Tailscale Serve's background setting keeps the proxy running, but does not start Onibi after a reboot.

## Verification boundaries

Hermetic backend tests cover native command construction, targeting failures, focus guards, cancellable/coalesced inventory, failure backoff, capability versions, HTTP authentication, origin/CSRF enforcement, and action recovery. Seven Node VM tests execute the actual JXA bridge with a simulated native object model. Chromium and WebKit UI fixtures exercise phone layouts, scope confirmations, draft isolation, output cancellation, readiness/recovery, and receipt recovery; axe checks automatically detectable WCAG A/AA issues.

Both browsers also run against the **production Go HTTP handler, embedded assets, real SQLite store, and real HTTPS cookies** with only the Ghostty backend simulated. That suite tests single-use pairing, origin/CSRF rejection, lost-response receipt recovery, deduplication, create/close reconciliation, rejected-action recovery, native/capture independence, version restrictions, a process killed during execution, restart revocation, and twelve-hour expiry. Its `browserfixture` build tag excludes the host from normal builds; configuration travels over test-process stdin, with no HTTP test backdoor. TLS uses an ephemeral local certificate trusted only by the test browser; this is not Tailscale certificate verification.

These fixtures do not execute Ghostty or establish physical iPhone/Tailscale behavior. Permission behavior and native responsiveness remain unverified on the actual device path. A passive local doctor check found Ghostty 1.3.1, one running process, and Accessibility authorization; it deliberately did not probe Automation or capture text.

The initial native smoke on Ghostty 1.3.1 verified inventory identities, but Ghostty then stopped answering automation requests during focus. Input and output capture were not reached, and cleanup also timed out. This is a preview with native end-to-end validation still outstanding; automated success alone does not establish that the local Ghostty installation responds to every control. Onibi reports interrupted actions as uncertain and does not restart Ghostty or replay them.

Run browser regressions with `cd web-tests && npm ci && npx playwright install chromium webkit && npm test && ONIBI_BROWSER=webkit npm test`. Go from `go.mod` is also required to build the integration fixture. Use `npm run test:integration` to run only the real-server suite. Both engines run in GitHub CI. Screenshots are written to a temporary directory printed by the UI test, or to `ONIBI_SCREENSHOT_DIR` if set. Set `ONIBI_CHROME_PATH` to use a local Chromium executable. The Node dependencies are test-only; the application remains a single Go binary with embedded assets. See [the readiness review](readiness-review.md) for this pass's scope and remaining decisions.
