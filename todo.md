# Onibi Remote Control TODO

Status date: April 2, 2026

This document is a handoff for an independent coding agent with no prior context. It describes:

- what is already implemented for the host control-plane work
- what still needs to be manually tested before Phase 01 can be called complete
- what follow-on scope exists in later phases
- which files matter for each area

Do not edit `README.md`. Keep existing `v1` monitoring endpoints working while extending the newer realtime control path.

## Repo Context

The repo contains:

- `Onibi/`: macOS menubar host app
- `OnibiCore/`: shared models and transport code
- `OnibiPhone/`: existing iPhone client
- `OnibiSessionProxy/`: local PTY-owning proxy executable
- `docs/phases/`: project phase specs

Relevant phase docs:

- `docs/phases/phase-01-host-control-plane.md`
- `docs/phases/phase-02-iphone-live-control.md`
- `docs/phases/phase-03-web-live-control.md`
- `docs/phases/phase-04-hardening-and-release.md`

Settings persistence detail:

- app settings are persisted through `Onibi/ViewModels/SettingsViewModel.swift`
- settings are stored in `UserDefaults`, not in a checked-in repo file
- the mobile-gateway and remote-control toggles should usually be changed through the app UI, not by editing source defaults

## Current Phase 01 State

Phase 01 is mostly implemented in code. The remaining work is primarily manual validation and a few integration/hardening gaps.

### Already implemented

Shared models and contracts:

- `OnibiCore/Models/ControllableSessionModels.swift`
- `OnibiCore/Models/RealtimeGatewayModels.swift`
- `OnibiCore/Models/LocalSessionProxyModels.swift`
- `OnibiCore/Services/RealtimeGatewayCodec.swift`

Host-side registry and buffering:

- `Onibi/Services/SessionOutputBuffer.swift`
- `Onibi/Services/ControllableSessionRegistry.swift`

Host-side HTTP and websocket gateway:

- `Onibi/Services/MobileGatewayService.swift`
- `Onibi/Services/RealtimeGatewayService.swift`
- `OnibiCore/Services/MobileGatewayRouter.swift`

Host-side local proxy listener and shell bootstrap path:

- `Onibi/Services/LocalSessionProxyListener.swift`
- `Onibi/Services/SessionProxyCoordinator.swift`
- `Onibi/Services/ShellHookInstaller.swift`
- `Onibi/OnibiApp.swift`

Session proxy executable:

- `OnibiSessionProxy/main.swift`
- `OnibiSessionProxy/SessionProxyRuntime.swift`
- `OnibiSessionProxy/RemoteInputByteTranslator.swift`

Manual probe:

- `scripts/manual_realtime_probe.swift`

Settings and UI exposure:

- `Onibi/Models/Settings.swift`
- `Onibi/Views/SettingsView.swift`
- `Onibi/Views/OnboardingView.swift`

### Implemented behavior

HTTP API:

- `GET /api/v2/bootstrap`
- `GET /api/v2/sessions`
- `GET /api/v2/sessions/:id/buffer`
- `POST /api/v2/sessions/:id/input`

Realtime API:

- websocket endpoint at `/api/v2/realtime`
- first-frame auth using pairing token
- session subscribe and unsubscribe
- request current buffer
- send text or key input
- receive live `session_added`, `session_updated`, `session_removed`, and `output` frames

Proxy path:

- shell bootstrap can `exec` into `OnibiSessionProxy`
- proxy owns PTY, forwards output to host, and accepts input frames
- host listener accepts proxy NDJSON messages over a unix domain socket

### Current automated verification

As of April 2, 2026:

- `swift test` passes with 231 tests
- `swift scripts/manual_realtime_probe.swift --help` prints usage and exits cleanly

Focused test coverage exists in:

- `OnibiTests/SessionOutputBufferTests.swift`
- `OnibiTests/ControllableSessionRegistryTests.swift`
- `OnibiTests/RealtimeGatewayServiceTests.swift`
- `OnibiCoreTests/MobileGatewayRouterTests.swift`
- `OnibiCoreTests/RealtimeGatewayCodecTests.swift`
- `OnibiSessionProxyTests/RemoteInputByteTranslatorTests.swift`
- `OnibiSessionProxyTests/SessionProxyLaunchConfigurationTests.swift`

### Not yet proven in reality

The code is implemented, but these things are still not verified on a real Ghostty-driven macOS flow:

- a brand-new Ghostty shell automatically re-execs through the proxy via installed shell hooks
- a real Ghostty session appears in the controllable session registry without manual bootstrapping
- text input, `Enter`, `Ctrl-C`, arrow keys, and space all work against a real interactive shell
- live output streaming behaves correctly for real shell usage
- token rotation, remote-control disablement, and disconnect behavior behave correctly with live clients

## Phase 01 Manual QA Still Required

This is the highest-priority remaining work before calling Phase 01 complete.

### Preconditions

Environment assumptions:

- macOS host machine
- Ghostty installed
- this repo checked out locally
- ability to run the app from the repo root

Important implementation detail:

- the shell hook embeds the resolved proxy binary path at install time
- build `OnibiSessionProxy` before installing or reinstalling shell hooks
- if you rebuild or move binaries, reinstall the shell hook so the embedded path is correct

### Step 1: Build and launch the host

From the repo root:

```bash
cd /Users/gongahkia/Desktop/coding/projects/onibi
swift build --product OnibiSessionProxy
swift run Onibi
```

Expected result:

- the menubar app launches
- no immediate startup error is shown

### Step 2: Enable host features in the UI

Open `Settings -> Mobile Access` in the macOS app.

Turn on:

- `Enable mobile gateway`
- `Enable remote control`

Verify in the same screen:

- `Gateway status` is `Running`
- `Local proxy listener` is `Running`
- `Proxy binary` is not `Unavailable`
- local endpoint shows `http://127.0.0.1:8787` unless the port was changed
- socket path is typically `~/.config/onibi/control.sock`

Copy the pairing token from the same screen.

### Step 3: Manual smoke test with one-off proxy bootstrap

Before testing the full shell-hook path, verify the proxy/gateway/realtime path directly.

Open a brand-new Ghostty tab and run:

```bash
cd /Users/gongahkia/Desktop/coding/projects/onibi
export ONIBI_SESSION_PROXY_ACTIVE=1
export ONIBI_PROXY_SOCKET_PATH="$HOME/.config/onibi/control.sock"
export ONIBI_HOST_SESSION_ID="$(uuidgen)"
export ONIBI_PARENT_SHELL="${SHELL:-/bin/zsh}"
export ONIBI_PARENT_SHELL_ARGS="-l"
export ONIBI_PROXY_VERSION="dev"
exec "$PWD/.build/debug/OnibiSessionProxy"
```

What this proves:

- the proxy binary starts
- it can connect to the local socket
- it can spawn the real shell
- it can stream data and receive input through the host

### Step 4: Confirm the host sees the controllable session

In another terminal:

```bash
export ONIBI_PAIRING_TOKEN='<paste token here>'

curl -s \
  -H "Authorization: Bearer $ONIBI_PAIRING_TOKEN" \
  http://127.0.0.1:8787/api/v2/bootstrap

curl -s \
  -H "Authorization: Bearer $ONIBI_PAIRING_TOKEN" \
  http://127.0.0.1:8787/api/v2/sessions
```

Expected result:

- bootstrap returns `featureFlags.remoteControlEnabled = true`
- bootstrap returns `featureFlags.websocketEnabled = true`
- sessions list contains at least one controllable session
- diagnostics show `controllableSessionCount >= 1`

If this fails:

- check the app’s `Remote Control Error` in settings
- confirm the socket file exists at `~/.config/onibi/control.sock`
- confirm the Ghostty shell really exec’d through the built proxy binary

### Step 5: Verify websocket auth, buffer fetch, output streaming, and input

Use the manual probe:

```bash
cd /Users/gongahkia/Desktop/coding/projects/onibi
swift scripts/manual_realtime_probe.swift \
  --token "$ONIBI_PAIRING_TOKEN" \
  --subscribe \
  --request-buffer \
  --text "pwd" \
  --key enter \
  --listen-seconds 5
```

Expected result:

- the probe prints bootstrap JSON
- the probe receives `auth_ok`
- the probe receives `sessions_snapshot`
- the probe receives `buffer_snapshot`
- the probe receives `input_accepted`
- the Ghostty shell runs `pwd`
- the probe receives live output from that command

### Step 6: Verify special keys against a real shell

Run these one at a time against the same controllable session:

```bash
swift scripts/manual_realtime_probe.swift --token "$ONIBI_PAIRING_TOKEN" --text "sleep 30" --key enter --listen-seconds 5
swift scripts/manual_realtime_probe.swift --token "$ONIBI_PAIRING_TOKEN" --key ctrl_c --listen-seconds 5
swift scripts/manual_realtime_probe.swift --token "$ONIBI_PAIRING_TOKEN" --key arrow_up --listen-seconds 5
swift scripts/manual_realtime_probe.swift --token "$ONIBI_PAIRING_TOKEN" --key arrow_down --listen-seconds 5
swift scripts/manual_realtime_probe.swift --token "$ONIBI_PAIRING_TOKEN" --key arrow_left --listen-seconds 5
swift scripts/manual_realtime_probe.swift --token "$ONIBI_PAIRING_TOKEN" --key arrow_right --listen-seconds 5
swift scripts/manual_realtime_probe.swift --token "$ONIBI_PAIRING_TOKEN" --key space --listen-seconds 5
```

Confirm:

- `Ctrl-C` interrupts a running command
- arrow keys affect shell history or cursor position
- space is inserted where expected
- output continues streaming after these actions

### Step 7: Verify the real shell-hook bootstrap path

After the one-off proxy smoke test passes, test the actual product path.

Current repo detail:

- shell hook generation is handled by `SessionProxyCoordinator.shared.hookScript(...)`
- install logic exists in `ShellHookInstaller.install(for:)`
- onboarding currently exposes copy-to-clipboard, not a polished install workflow

Recommended manual path:

1. Use the app onboarding flow to copy the shell hook script, or call the installer from code if you add temporary local tooling.
2. Paste the generated zsh block into `~/.zshrc`.
3. Fully quit Ghostty.
4. Open a brand-new Ghostty tab.
5. Run the `curl` or probe checks again.

Expected result:

- the new Ghostty shell becomes controllable without manually running `OnibiSessionProxy`
- the session appears via `GET /api/v2/sessions`
- live output and remote input behave the same as the one-off smoke test

### Step 8: Failure-mode QA still needed

These scenarios should be manually verified even if the happy path works:

1. Invalid HTTP token:
   - `GET /api/v2/bootstrap` with a bad bearer token should return `401`

2. Invalid websocket token:
   - connect with the probe but provide a bad `--token`
   - expect auth failure and socket close

3. Token rotation while clients are active:
   - connect a live realtime client
   - rotate the pairing token in the app
   - define and verify the intended behavior
   - current implementation likely needs further hardening here

4. Disable remote control while the app is running:
   - turn `Enable remote control` off
   - confirm the local proxy listener stops
   - confirm old shells degrade gracefully

5. Disable mobile gateway while the app is running:
   - turn `Enable mobile gateway` off
   - confirm HTTP and websocket access stop cleanly

6. Host restart:
   - stop the app, relaunch it, and repeat the probe flow

7. Proxy disconnect:
   - close the Ghostty tab that was proxied
   - confirm the session becomes stale or disappears as intended
   - confirm diagnostics reflect the removal

8. Host sleep/wake:
   - put the Mac to sleep with an active proxied session
   - wake it and verify registry cleanup and reconnect behavior

## Phase 01 Engineering Gaps Still Worth Closing

These items are adjacent to Phase 01 and should be considered before moving too far forward.

### 1. Add end-to-end integration tests for the local proxy path

Missing high-value coverage:

- proxy registration -> session visible in registry
- output frame -> buffer snapshot -> websocket broadcast
- websocket `send_input` -> routed to the correct session input handler
- invalid websocket auth -> error and disconnect
- disconnected proxy -> stale session cleanup

Likely files:

- `OnibiTests/RealtimeGatewayServiceTests.swift`
- new host-side proxy integration tests in `OnibiTests/`

### 2. Clarify token rotation semantics for realtime clients

Phase 04 explicitly requires this, but it is useful to pin down earlier:

- old HTTP token must fail immediately
- old websocket auth must fail immediately
- existing live websocket sessions authenticated before rotation should probably be closed

Current implementation does not appear to force-close already authenticated websocket clients on token rotation.

### 3. Add clearer shell-hook install UX

Current state:

- install logic exists in `ShellHookInstaller`
- onboarding only exposes copy-to-clipboard

Recommended future improvement:

- add an explicit install and uninstall UI in settings
- show current zsh, bash, and fish install status
- allow reinstall when the proxy binary path changes

This is not required to prove the transport works, but it will materially improve manual QA and user reliability.

### 4. Expand host diagnostics

Diagnostics already expose:

- controllable session count
- connected realtime client count
- proxy registration failure count
- stale session count
- local proxy socket health

Still useful to add later:

- websocket auth failure count
- proxy disconnect count
- token rotation timestamp
- buffer truncation count
- last input routing error

## Future Scope After Phase 01

The sections below are not required to close Phase 01, but they are the intended next implementation stages.

## Future Scope: Phase 02

Source of truth:

- `docs/phases/phase-02-iphone-live-control.md`

Goal:

- upgrade the iPhone app from polling-only monitoring into a realtime live-control client

### What to implement

New shared transport and view-model code in `OnibiCore`:

- `OnibiCore/Services/RealtimeGatewayClient.swift`
- `OnibiCore/ViewModels/ControllableSessionsViewModel.swift`
- `OnibiCore/ViewModels/LiveSessionViewModel.swift`

New iPhone UI files in `OnibiPhone`:

- `OnibiPhone/LiveSessionView.swift`
- `OnibiPhone/RemoteInputBar.swift`
- `OnibiPhone/RemoteKeyStrip.swift`
- `OnibiPhone/SessionOutputView.swift`

Upgrade the app flow so it:

- uses `GET /api/v2/bootstrap` on startup or foreground
- opens websocket only after bootstrap succeeds
- maintains a session list from bootstrap plus realtime deltas
- opens a live session view with buffered output plus live output
- sends text and special keys through the realtime API

### Detailed testing required for Phase 02

Automated tests:

- websocket message decoding and encoding
- reconnect backoff behavior
- session delta merge logic
- transcript append behavior
- transcript truncation-banner state
- input frame construction for text and special keys
- opening a session triggers subscribe plus buffer request
- session removal while open updates the live-session state

Manual QA:

1. Pair iPhone app against a Phase-01-capable host.
2. Confirm bootstrap loads controllable sessions.
3. Open a live session and verify initial buffer appears.
4. Send text input and see live output.
5. Send `Ctrl-C`.
6. Send arrow keys.
7. Background and foreground the app and verify reconnect.
8. Rotate token on the host and confirm the app surfaces re-pairing requirements.

### Constraints for the implementing agent

- do not redesign the app into a terminal emulator
- preserve the current visual tone
- keep most transport logic in `OnibiCore`, not `OnibiPhone`
- do not add background push in this phase

## Future Scope: Phase 03

Source of truth:

- `docs/phases/phase-03-web-live-control.md`

Goal:

- introduce a web client that uses the same host API as the iPhone app

### What to implement

Create a new web workspace:

- `OnibiWeb/`

Recommended initial files:

- `OnibiWeb/package.json`
- `OnibiWeb/tsconfig.json`
- `OnibiWeb/vite.config.ts`
- `OnibiWeb/src/main.tsx`
- `OnibiWeb/src/App.tsx`
- `OnibiWeb/src/styles.css`
- `OnibiWeb/src/api/realtimeClient.ts`
- `OnibiWeb/src/store/sessionStore.ts`
- `OnibiWeb/src/views/ConnectionView.tsx`
- `OnibiWeb/src/views/SessionsView.tsx`
- `OnibiWeb/src/views/LiveSessionView.tsx`
- `OnibiWeb/src/components/RemoteInputBar.tsx`
- `OnibiWeb/src/components/RemoteKeyStrip.tsx`
- `OnibiWeb/src/components/SessionOutputPane.tsx`

Host-side serving work:

- serve static web assets from the macOS host
- keep `/api/v2/*` and `/api/v2/realtime` on the same host

Browser features:

- save base URL and pairing token locally
- bootstrap over HTTP
- auth over websocket first frame
- session list screen
- live session screen
- special key strip
- mobile and desktop browser support

### Detailed testing required for Phase 03

Automated tests:

- bootstrap request handling
- websocket auth rejection handling
- reconnect behavior on transient network loss
- no reconnect loop on hard unauthorized failure
- transcript append behavior without full rerender churn

Manual QA:

1. Pair the web app against the host over the Tailscale-served URL.
2. Test on desktop browser.
3. Test on mobile Safari.
4. Test on Android Chrome.
5. Open a live session and send text plus special keys.
6. Confirm output auto-scroll behavior and jump-back-to-live behavior.

### Constraints for the implementing agent

- do not add a cloud backend
- do not invent a web-only API
- keep the design intentionally simple and operational
- ensure the host bundles or deterministically locates web assets

## Future Scope: Phase 04

Source of truth:

- `docs/phases/phase-04-hardening-and-release.md`

Goal:

- harden the remote-control system so it is reliable enough for everyday personal use

### What to implement

1. Feature flags and kill switches:
   - `remoteControlEnabled`
   - `webClientEnabled`
   - `realtimeGatewayEnabled`

2. Version compatibility:
   - host version in `auth_ok`
   - protocol version in realtime messages if needed
   - proxy version mismatch reporting and enforcement

3. Diagnostics and observability:
   - websocket auth failures
   - proxy disconnect count
   - buffer truncation count
   - last token rotation time
   - last input routing error

4. Session lifecycle cleanup:
   - heartbeat timeout handling
   - exited-session grace period
   - clean websocket unsubscribe behavior on session removal
   - buffer purge for expired sessions

5. Token rotation behavior:
   - invalidate old HTTP requests
   - reject new websocket auth with old token
   - close existing authenticated websocket sessions that used the old token

6. Shell bootstrap fail-open:
   - if proxy bootstrap fails, local shell must still open normally
   - remote control may degrade to unavailable, but local Ghostty use must not break

7. Web asset synchronization:
   - host diagnostics show served web build version
   - web app shows host version and protocol version
   - missing web assets surface clearly in diagnostics

### Detailed testing required for Phase 04

Host automated tests:

- stale session cleanup
- proxy version mismatch handling
- token rotation invalidates existing websocket sessions
- shell bootstrap fail-open path
- diagnostics counters update as expected

Client automated tests:

- iPhone unauthorized state after token rotation
- iPhone reconnect stops on explicit unauthorized
- web unauthorized bootstrap handling
- web websocket auth rejection handling

Manual QA matrix:

1. macOS host + iPhone over Tailscale
2. macOS host + mobile Safari over Tailscale
3. macOS host + Android Chrome over Tailscale
4. host sleep/wake while remote session is open
5. token rotation while iPhone and web are connected
6. broken proxy bootstrap while confirming local Ghostty still works

## Practical Next Recommended Order

If you are the next agent picking this up, do the work in this order:

1. Finish Phase 01 manual QA using the exact commands in this document.
2. Fix any real Ghostty/bootstrap bugs found during that QA.
3. Add the highest-value missing host integration tests.
4. Move to Phase 02 and keep transport code shared in `OnibiCore`.
5. Only after Phase 02 is stable, start Phase 03 web work.
6. Use Phase 04 as the reliability pass after both clients exist.

## Definition Of "Phase 01 Complete"

Treat Phase 01 as complete only when all of the following are true on a real macOS + Ghostty machine:

- a new Ghostty shell becomes a controllable session automatically through installed shell hooks
- the session appears in `GET /api/v2/sessions`
- realtime auth succeeds via `/api/v2/realtime`
- buffered output and live output arrive remotely
- remote text input works
- `Enter`, `Ctrl-C`, arrow keys, and space work
- existing `v1` monitoring endpoints still behave normally
- diagnostics are sufficient to debug failures

Until that is proven manually, Phase 01 should be considered code-complete but not product-complete.
