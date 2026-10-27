# Ghostty web control

Onibi's web interface controls the Ghostty application already running in your Mac's graphical login session. It does not move processes into a new terminal, add hooks to running coding agents, or keep the Mac awake. The Mac, Ghostty, and the Onibi web process must remain available.

The cockpit is served by the Mac over private Tailscale HTTPS. This repository does not use GitHub Pages: the former `/docs` publishing configuration was retired. Documentation stays in the repository and release archives; Pages is not part of deployment or CI verification.

## Build and run from source

From this checkout, use the Go toolchain declared in `go.mod` and Apple's Command Line Tools (`xcode-select --install` if missing):

```sh
make build
./bin/onibi web
```

`make build` enables CGO on macOS, compiles both binaries into a private staging directory, verifies the executable, and replaces the local `bin/` artifacts. It shows four stages, elapsed time, and actual compiler package activity. The indeterminate animation is a Go adaptation of [Rattles](https://github.com/vyfor/rattles/tree/6eabf671ee86ddd773f458b9c25ef9d3f00ae53b); it does not invent a percentage. Rust is not required. The shell launcher prints the banner immediately, before the small Go helper bootstraps. A cold Go cache can make preparation take longer, but does not delay the banner.

The raw `CGO_ENABLED=1 go build -o bin/onibi ./cmd/onibi` command still works, but Go itself does not use Onibi's presentation. Use `make build` for progress, provenance, staging and recovery. `VERBOSE=1 make build` streams compiler output; `ONIBI_PLAIN=1 make build` disables animation and block art. Redirected output and CI use plain stage lines. Compiler output is retained in `bin/build.log` with mode `0600`, replaced on each build; failed stages print its last lines. Build failures preserve previous binaries. Cancellation stops the compiler process group.

The foreground server prints one aligned ONIBI block banner with ANSI dim shading, followed by revision, PID, listener, log path, a copyable pairing URL and commands using the current executable. Both `make build` and `make run` print that banner first, before `[prepare]` or compiler output. Build stages then use a compact heading. `make run` passes banner ownership to the web process so it does not print a second logo after compilation. A separate `./bin/onibi web` invocation still prints its own banner. Status, log viewing, source install/update/remove, and `make help` use the same banner. It does not probe Ghostty at startup. Keep that terminal open and use a second terminal for status or logs. Ctrl-C drains bounded work before exiting. If an old Onibi process is already running, stop it explicitly and launch the new binary to see the new interface; a build does not restart it.

`make run` builds and starts the web cockpit; `make doctor` builds and runs passive web diagnostics; `make setup` adds startup guidance. `make help` lists the source workflow. `make test` runs hermetic race-enabled Go tests.

## Install, update and remove

Source-based installation is optional. Running `./bin/onibi web` directly is supported.

```sh
make install DRY_RUN=1
make install
make install-status
```

The default destination is `~/.local/bin`; use `INSTALL_DIR='/path/with spaces/bin'` consistently to choose another directory. No `sudo` is needed. Installation prints the PATH addition if needed, but does not edit shell configuration or start a service. Shell completion is available through `onibi completion zsh` (also `bash` and `fish`).

The private `.onibi-install.json` manifest records the source checkout, revision and checksums of `onibi` and `onibi-notify`. Existing unmanaged, modified, missing or symlinked binaries are refused with an actionable error. The two binaries and manifest are staged on the destination filesystem. Each replacement is atomic, with rollback if a subsequent replacement fails; this is not a single multi-file transaction across power loss. A lock prevents concurrent lifecycle operations; the build lock remains held while installation copies the exact compiled artifacts. An incomplete rollback retains recovery copies and reports their path.

```sh
make update DRY_RUN=1              # preview rebuilding this checkout
make update                        # compile and install this checkout
make update UPDATE_PULL=1          # optionally fetch and fast-forward clean main
make update RESTART_WEB=1          # explicitly restart the web login service
```

`make upgrade` is an alias for `make update`. Pulling requires clean `main`, including untracked files, and refuses divergence; it never resets or rebases local work. If a pull succeeds but compilation fails, the source stays advanced while the installed binaries remain unchanged. Updates preserve state, remembered browser grants, configuration, credentials and logs. Running processes keep their current executable until explicitly restarted. A requested restart failure is reported separately after successful binary installation. Packaged release downloads and automatic background updates are outside this workflow.

```sh
make uninstall DRY_RUN=1
make uninstall
```

Uninstall previews the exact owned paths. It verifies hashes, stops/removes only login services pointing to this managed installation, and refuses services from another installation. A service stop failure retains its configuration and the installed binaries. Foreground processes must be stopped separately with Ctrl-C. Only managed binaries and their manifest are removed; unrelated files, the checkout, Ghostty terminals, state and logs remain. Browser grants remain in the state database for a later installation. To retire access too, run `onibi web devices revoke-all` while the server is still running before uninstalling. There is no implicit state purge.

## Status and logs

```sh
./bin/onibi version --json          # metadata for this executable
./bin/onibi web status              # metadata for the running server
./bin/onibi web status --json
./bin/onibi web logs --tail 50
./bin/onibi web logs --json --tail 100
```

To keep a separate log viewer open:

```sh
./bin/onibi web logs --follow
```

Follow deliberately waits for new events, including when the server is stopped. **Ctrl-C exits the viewer**; it does not stop a server running in another terminal. Commands pasted after `--follow` wait until it exits. Use `web logs --tail 50` for a snapshot that returns immediately.

Status uses the same-user local administration socket. It reports process/version, uptime, origin, request/error totals, active requests, completed/rejected/uncertain action totals, and last observed inventory/capture health. It does not invoke Ghostty. Counters reset with the process; native observations can be unknown or stale. A replayed durable receipt is not counted as a new dispatch. A running/local revision mismatch is called out. Old running versions need an explicit restart before they support these commands. A missing/refused administration socket produces a normal **Stopped** screen with startup guidance; status exits successfully, and `--json` reports `{"running":false,"state":"stopped"}`. Running JSON status adds `"running":true` to the existing runtime fields. Permission errors and timeouts still fail; they are not reported as stopped.

The web server writes JSON events to `~/Library/Application Support/onibi/logs/web.log` (`0600`), rotating during operation at 1 MiB with three backups. Foreground mode also prints compact timestamped events, using the same formatting as log replay. An animated activity row stays below the events, showing elapsed time and request counts. **HTTP live** describes the local web process, not a claim that Ghostty or the phone connection is healthy. A separate log viewer shows **Following** and the number of records shown. These activity frames are presentation only and are never added to the log file. Long fields and event metadata wrap with indentation; narrow footers keep the exit gesture before secondary counters. `--log-level debug` adds successful read-request metadata; normal polling is quiet at the default `info` level. HTTP responses include `X-Onibi-Request-ID` to correlate a request with its event. Action logs contain the kind and recorded outcome; native logs record availability transitions and elapsed time.

Telemetry uses allowlisted metadata. It excludes terminal input/output, target identities/titles, device names, raw URLs/query strings, receipt IDs, cookies, CSRF values and pairing tokens, including in debug mode. Pairing links appear only in explicit foreground/pair command output. Redaction is a secondary defense, not a reason to log payloads. Build logs contain compiler diagnostics rather than application events.

Log inspection reads at most the last 1 MiB and 1,000 records initially, refuses symlinks/nonregular files, and follows rotation until Ctrl-C. It is a local troubleshooting view, not guaranteed archival delivery across rapid rotations. Use `--json` for tooling and `--plain` (or `ONIBI_PLAIN=1`) for terminal presentation without animation/art. `NO_COLOR=1` disables ANSI shading while retaining interactive progress. Non-UTF-8 locales get ASCII glyphs; locale overrides follow `LC_ALL`, `LC_CTYPE`, then `LANG`. Redirected/CI/dumb-terminal output stays plain. No alternate screen is used, and the activity row is removed before the command returns to the shell. `web service status` inspects launchd registration separately from `web status`, which inspects the actual running HTTP process.

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

The printed link contains a one-use pairing secret in its URL fragment. Open it on the intended iPhone browser within ten minutes. The page removes the fragment immediately, then asks you to name the browser and choose its access duration before connecting. Temporary access is the default: it lasts up to twelve hours and ends when Onibi restarts. **Remember this browser for 30 days** is an explicit option with a fixed expiry; reconnecting does not extend it. Credentials use HttpOnly cookies and CSRF stays in memory. A manual token field is available. Terminal text, drafts, and pairing secrets are never stored in browser local storage; only reader size/wrap preferences are saved there.

For local development, omit `--origin` and use the printed loopback HTTP link on the Mac. For phone access, use the configured HTTPS origin consistently; switching between the loopback and private addresses does not share a browser session.

## Mac readiness

Start with a passive check in the same launching environment as the web process:

```sh
./bin/onibi web doctor
./bin/onibi web doctor --json --origin https://your-mac.your-tailnet.ts.net
```

This inspects the Ghostty app bundle/version, running-process count, and the calling process's Accessibility authorization. It validates listener/origin syntax without binding a port or contacting Tailscale. It sends no Ghostty Apple events, requests no permission prompts, changes no focus, and reads no terminal text. Automation remains **unknown** until a native request is made. Unknown is not proof of readiness; a successful exit means no known failing check, not a verified end-to-end connection. Known failures return a nonzero exit code and still print the full JSON report when requested.

When ready to test native responsiveness, explicitly run `onibi web doctor --probe`. This sends one bounded inventory request and can prompt for Automation permission. Adding `--read-target '<exact pane ID>'` verifies focused-pane capture and discards the captured text; it never focuses the pane for you. Do not add these options when you only want passive diagnostics.

On the phone, tap the connection status to open **Mac readiness**. **Explore cockpit capabilities** lists implemented controls, automated contract coverage, version requirements, focus requirements, native observations, and unsupported features. The endpoint is passive: it does not probe Ghostty. A command acknowledgement is not proof that its program completed work; fake backends cannot acquire native-verification labels. Observation evidence lasts only for this Onibi run. The map explicitly excludes semantic agent/approval detection, mouse forwarding, full terminal emulation, and complete or unfocused scrollback capture.

Mac readiness shows passive permission/process details, the installed version, the last native inventory duration, and browser session expiry. **Check again** refreshes diagnostics and requests inventory when its retry delay permits. Observations describe the last completed request, not a guarantee about the next one.

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
- Close a pane, tab, or window after reviewing its exact affected pane list. The server and native bridge reject the close if membership changed. A successful native acknowledgement triggers at most three inventory observations within three seconds: the phone reports **Close confirmed**, **Still open on your Mac**, or **Close outcome unverified**. Confirmation means the reviewed panes disappeared; an empty native window object is not independently verified. A local Ghostty prompt may keep work open. Observation never repeats the close. Checks and the close remain separate OS calls, so a narrow native race cannot be eliminated.
- Drafts belong to their exact pane and stay only in page memory. Switching panes restores that pane's draft; a removed pane's draft never transfers to another shell. Network loss or session expiry preserves page-memory drafts. Reloading, closing the page, or choosing **Forget browser** discards them. Delivery recovery stores only an unresolved receipt and destination IDs.
- **Reader tools** changes only this browser: 12–22px text, optional wrap, and start/end navigation within the captured snapshot. **Expand** opens a focused reading view with the destination, capture age, and a visible Done button. **Copy text** copies literal snapshot text; when clipboard access is denied, it selects the text for browser Copy. Copying or touching output pauses Follow so selection is stable. Clipboard writes need browser support and permission; copied content then belongs to the system clipboard. Reader controls do not change the Mac font, native scroll position, or capture bounds.
- **Follow** requests fresh visible text 2.5 seconds after the previous request settles. Requests do not overlap. Switching panes stops Follow; hiding the page pauses it, and returning reads again. Capture errors or going offline stop it until you explicitly enable it again. Output age is visible, and the view preserves your scroll position unless you were already following the bottom. Follow cannot read an unfocused pane or keep your Mac awake.

Ghostty's [AppleScript API](https://ghostty.org/docs/features/applescript) controls native terminals by identity. Stock Ghostty 1.3 exposes visible text through Accessibility, but not through a UUID-addressable AppleScript text property. Onibi therefore avoids guessing a pane from its title or screen position. See the [Ghostty Accessibility implementation](https://github.com/ghostty-org/ghostty/blob/v1.3.1/macos/Sources/Ghostty/Surface%20View/SurfaceView_AppKit.swift).

macOS may require Automation permission to control Ghostty and Accessibility permission to read terminal text. Use System Settings → Privacy & Security for the application or process macOS identifies as launching Onibi. Capture requires a native macOS build with CGO enabled; a CGO-disabled build reports capture unavailable while retaining supported controls.

## Delivery and recovery

Each action has a unique receipt recorded before execution. Reusing the same ID and payload returns its prior state; changing the payload under an existing ID is rejected. Network loss causes the browser to check the receipt, never silently resend the input. An action interrupted by a crash or an ambiguous native error is marked uncertain. Inspect the terminal before deciding whether to send a new action.

Receipts retain hashes and decision metadata, not command text. The browser keeps only an unresolved receipt’s ID, action kind and destination IDs in tab-scoped session storage, so a reload can check its outcome. Restart marks unfinished receipts uncertain. Reconnection checks receipts without replaying commands. Only one web process can use a state directory at a time.

## Browsers and local recovery

Run these commands on the Mac while the web server is running:

```sh
onibi web pair
onibi web devices list
onibi web devices list --json
onibi web devices revoke '<device-id>'
onibi web devices revoke-all
```

`pair` prints a fresh ten-minute, one-use link without restarting. It invalidates any unused prior link and preserves existing paired devices. Treat the link as a credential. The local administration socket lives in the private state directory, has mode 0600, verifies peer user IDs, accepts no terminal actions, and has no HTTP equivalent for issuing links.

**Devices** on the phone lists active named browser grants and individually revokes them after confirmation. Names are user labels, not verified hardware identities. **Forget browser** revokes this browser, clears its cookies, and discards page-memory drafts. Revocation invalidates short sessions and remembered renewal, including commands still queued or validating their target. It cannot recall commands already dispatched.

Remembered access uses a separate Secure/HttpOnly/SameSite Strict cookie over HTTPS. The Mac stores only its hash, name, opaque ID, origin, creation time and fixed expiry. A GET to `/api/session` may establish a fresh short session and CSRF token after a restart; an expired POST is rejected, never replayed. **Reconnect a remembered browser** restores access without discarding page-memory drafts, but you still choose a pane and explicitly send again. Temporary devices are removed on restart. Remembered devices expire after 30 days, independent of reconnects, and are restricted to the configured origin.

`revoke-all` is local-only: it revokes all browsers for the configured origin and invalidates the unused pairing link. Restart alone does **not** revoke remembered access. Stop the foreground web process or its login service to cut off browser control while it is stopped. In-flight commands may finish during shutdown; existing Ghostty terminals continue running. Removing the service does not delete the state database or device grants; revoke them first if you want to retire access permanently.

## Background web service

Use a stable installed binary, then configure the separate Mac login service:

```sh
onibi web service install --origin https://your-mac.your-tailnet.ts.net
onibi web service start
onibi web pair
onibi web service status
onibi web service status --json
onibi web service restart
onibi web service stop
onibi web service remove
```

Install writes a private `~/Library/LaunchAgents/sh.onibi.web.plist`; it does not start the process immediately. Start launches it in the current user’s graphical session. It also starts at the next graphical login. Stop unloads the job until another start or login; remove also deletes its plist. Existing installations must be removed before changing their stored address. Restart uses stop/start, with a 45-second shutdown allowance for bounded commands and receipt recording. A fixed loopback port is required; `--listen` defaults to `127.0.0.1:8787`.

Application events go to the continuously rotated `web.log`. Launchd retains bootstrap errors in `web.err.log` and `web.out.log`; these diagnostic streams are separate from application telemetry. Background mode prints no pairing secrets; retrieve a fresh link locally. A foreground web process must be stopped before starting the service against the same state directory. macOS permissions are tied to the launching environment; inspect readiness again when switching to the login service. Live launchd/TCC behavior has not been validated in this pass.

`onibi system service install` manages the separate managed-session daemon, which the web cockpit does not use; see [managed sessions](managed-sessions.md). Tailscale Serve’s background setting keeps the proxy running; it does not start Onibi or keep the Mac awake. A graphical login, running Ghostty, and an awake reachable Mac remain necessary.

## Verification boundaries

Hermetic backend tests cover native command construction, targeting failures, focus guards, cancellable/coalesced inventory, failure backoff, capability versions, HTTP authentication, origin/CSRF enforcement, and action recovery. Eight Node VM tests execute the actual JXA bridge with a simulated native object model. Chromium and WebKit UI fixtures exercise phone layouts, scope confirmations, draft isolation, output cancellation, readiness/recovery, and receipt recovery; axe checks automatically detectable WCAG A/AA issues.

Both browsers also run against the **production Go HTTP handler, embedded assets, real SQLite store, and real HTTPS cookies** with only the Ghostty backend simulated. That suite tests single-use pairing, origin/CSRF rejection, lost-response receipt recovery, deduplication, create/close reconciliation, rejected-action recovery, native/capture independence, version restrictions, a process killed during execution, temporary restart revocation, twelve-hour expiry, opt-in remembered access across process restarts, individual revocation, fixed 30-day expiry, capability/device accessibility, and close-inventory races. Its `browserfixture` build tag excludes the host from normal builds; configuration travels over test-process stdin, with no HTTP test backdoor. TLS uses an ephemeral local certificate trusted only by the test browser; this is not Tailscale certificate verification.

These fixtures do not execute Ghostty or establish physical iPhone/Tailscale behavior. Permission behavior and native responsiveness remain unverified on the actual device path. A passive local doctor check found Ghostty 1.3.1, one running process, and Accessibility authorization; it deliberately did not probe Automation or capture text.

The initial native smoke on Ghostty 1.3.1 verified inventory identities, but Ghostty then stopped answering automation requests during focus. Input and output capture were not reached, and cleanup also timed out. This is a preview with native end-to-end validation still outstanding; automated success alone does not establish that the local Ghostty installation responds to every control. Onibi reports interrupted actions as uncertain and does not restart Ghostty or replay them.

Run browser regressions with `cd web-tests && npm ci && npx playwright install chromium webkit && npm test && ONIBI_BROWSER=webkit npm test`. Go from `go.mod` is also required to build the integration fixture. Use `npm run test:integration` to run only the real-server suite. Both engines run in GitHub CI. Screenshots are written to a temporary directory printed by the UI test, or to `ONIBI_SCREENSHOT_DIR` if set. Set `ONIBI_CHROME_PATH` to use a local Chromium executable. The Node dependencies are test-only; the application remains a single Go binary with embedded assets. See [the readiness review](readiness-review.md) for this pass's scope and remaining decisions.
