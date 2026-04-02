# Phase 02: Native iPhone Live Session Control

## Objective

Upgrade the existing iPhone app from a polling-only monitor into a first-class live remote control client for Onibi's new host control plane.

At the end of this phase, the iPhone app must be able to:

- connect to the host using the same pairing token model it already uses
- establish a websocket session against the new realtime API
- show the current controllable session list
- open a live session
- render buffered and live output for that session
- send text, `Enter`, `Ctrl-C`, arrow keys, and space to that session
- recover cleanly from temporary disconnects

The UI should remain simple and dependable. Do not redesign the app into a terminal emulator.

## Non-Negotiable Constraints

- Do not edit `README.md`.
- Reuse the existing connection model: Tailscale base URL + pairing token.
- Preserve the current visual tone in `OnibiPhone/`; do not introduce an over-designed UI.
- Keep the current read-only dashboard and diagnostics useful even while adding live control.
- Do not implement background push in this phase.
- Live updates only need to work while the app is foregrounded and connected.

## Current Repo Context

Relevant files already present:

- `OnibiPhone/OnibiPhoneApp.swift`
- `OnibiPhone/PhoneRootView.swift`
- `OnibiPhone/PhoneDashboardView.swift`
- `OnibiPhone/PhoneSessionsView.swift`
- `OnibiPhone/ConnectionSetupView.swift`
- `OnibiCore/ViewModels/MobileMonitorViewModel.swift`
- `OnibiCore/Services/MobileAPIClient.swift`
- `OnibiCore/Services/MobileConnectionStore.swift`

Current behavior:

- the iPhone app stores a base URL and pairing token
- it polls the host every 5 seconds using `MobileMonitorViewModel`
- sessions are read-only snapshots with command history previews

After Phase 01, the iPhone app should move from polling-only behavior toward a hybrid model:

- HTTP bootstrap for initial state
- websocket for live session updates and input/output

## Recommended Shared Code Strategy

Keep most transport logic in `OnibiCore/` so the iPhone app and future web client converge on the same contract.

### New shared files in `OnibiCore/`

- `OnibiCore/Services/RealtimeGatewayClient.swift`
- `OnibiCore/ViewModels/LiveSessionViewModel.swift`
- `OnibiCore/ViewModels/ControllableSessionsViewModel.swift`

### New iPhone-specific files in `OnibiPhone/`

- `OnibiPhone/LiveSessionView.swift`
- `OnibiPhone/RemoteInputBar.swift`
- `OnibiPhone/RemoteKeyStrip.swift`
- `OnibiPhone/SessionOutputView.swift`

If the existing `PhoneSessionsView.swift` becomes too large, split it. Do not force everything into that file.

## Transport Expectations

## HTTP bootstrap

On app launch or foreground entry:

1. load the saved connection configuration from `MobileConnectionStore`
2. call `GET /api/v2/bootstrap`
3. populate the dashboard and session list from the bootstrap payload
4. open the websocket connection only after bootstrap succeeds

## WebSocket lifecycle

Recommended client behavior:

1. connect to `/api/v2/realtime`
2. send the auth frame immediately
3. wait for `auth_ok`
4. request a sessions snapshot if the server does not push one immediately
5. subscribe to session list changes
6. when the user opens a session, subscribe to that session and request its buffer snapshot
7. maintain the socket while the app is active
8. disconnect when the app backgrounds

### Reconnect policy

Use simple exponential backoff.

Recommended backoff sequence:

- 1 second
- 2 seconds
- 4 seconds
- 8 seconds
- cap at 15 seconds

Reset the backoff after any successful authenticated connection.

## View Model Requirements

## `ControllableSessionsViewModel`

This view model should own:

- current connection state
- bootstrap state
- websocket lifecycle state
- current controllable sessions list
- high-level errors
- last successful sync timestamp

Recommended connection states:

- `notConfigured`
- `bootstrapping`
- `connectingRealtime`
- `online`
- `degraded`
- `unauthorized`
- `unreachable`
- `failed(String)`

### Responsibilities

- merge bootstrap session snapshots and websocket deltas
- expose sorted sessions for the list UI
- track whether a session is live/controllable
- expose simple retry actions

## `LiveSessionViewModel`

This view model should be scoped to one session.

It should own:

- current session metadata
- output transcript state
- buffer cursor
- pending input text
- send state for manual text sends
- error banners specific to the open session
- websocket subscription lifecycle for the session

### Responsibilities

- request the initial buffer snapshot when opened
- append incoming output chunks in order
- keep the output view pinned to bottom unless the user scrolls away
- send text input
- send key presses
- surface session exit or disconnect clearly

## UI Requirements

## Sessions list

Update the current iPhone sessions screen so each row can show:

- session display name
- live or disconnected state
- whether the session is controllable
- last activity time
- last command preview if available

Recommended row actions:

- tap row -> open live session detail

### Empty states

Provide distinct empty states for:

- no configured connection
- configured but no controllable sessions
- realtime disconnected but cached bootstrap data exists

## Live session screen

Keep the layout minimal.

Recommended structure:

1. compact session status header
2. large scrollable monospaced output area
3. fixed bottom input area
4. small key strip for special keys

### Status header fields

- session name
- transport state
- last activity
- working directory if available

### Output view

Requirements:

- monospaced font
- preserve line breaks
- do not try to render ANSI color in v1 unless it is trivial and stable
- support large outputs without re-rendering the entire transcript on every chunk
- show a subtle banner when older buffer content was truncated on the host

### Input bar

Requirements:

- text field for line-based input
- send button for text payloads
- separate buttons for `Enter`, `Ctrl-C`, and space if needed
- arrow key strip for up/down/left/right

Do not implement full terminal keyboard handling in v1.

## Minimal Interaction Contract

### Text send

When the user types `npm test` and taps `Send`, the app should send:

```json
{ "type": "send_input", "sessionId": "...", "kind": "text", "text": "npm test" }
```

### Enter key

```json
{ "type": "send_input", "sessionId": "...", "kind": "key", "key": "enter" }
```

### Ctrl-C key

```json
{ "type": "send_input", "sessionId": "...", "kind": "key", "key": "ctrl_c" }
```

### Arrow keys

Use the same server frame schema with `key` set to the corresponding arrow key value.

## Recommended Implementation Tasks

## Task 1: Add realtime client service in `OnibiCore`

Create a websocket-capable transport layer in `OnibiCore` so iPhone and web can share the same contract later.

Implementation requirements:

- use `URLSessionWebSocketTask`
- support auth handshake as the first outbound frame
- decode server messages into strongly typed `RealtimeServerMessage`
- expose async callbacks or an observable stream for higher-level view models
- support sending typed `RealtimeClientMessage` frames

## Task 2: Add session-oriented shared view models

Implement `ControllableSessionsViewModel` and `LiveSessionViewModel` in `OnibiCore`.

They should be usable by the iPhone target directly and should not depend on SwiftUI view types.

## Task 3: Upgrade connection flow

Modify the current iPhone startup flow so it:

- still uses `MobileConnectionStore`
- calls the new bootstrap endpoint
- starts the realtime client only after bootstrap succeeds
- stops the realtime client on scene backgrounding

## Task 4: Replace read-only session detail with live control screen

Refactor or replace the current session detail flow in `OnibiPhone/PhoneSessionsView.swift`.

Do not try to preserve the old read-only command preview screen as the main detail screen. The live session screen becomes the primary detail path for controllable sessions.

## Task 5: Implement output transcript rendering

Build a transcript renderer that can append chunks incrementally.

Guidelines:

- represent transcript as an append-only list of chunks or lines
- avoid building a giant single string if that causes visible lag
- if needed, coalesce adjacent chunks before display
- keep the implementation simple and testable

## Task 6: Implement input controls

The input controls should map directly to the host protocol.

Include:

- free text send
- `Enter`
- `Ctrl-C`
- arrow strip
- space input path

If text entry already supports spaces, do not add an extra visible space button unless it improves usability.

## Task 7: Error handling and degraded mode

Handle these states explicitly:

- bootstrap failed but cached connection exists
- websocket disconnected while viewing a session
- server rejects auth after token rotation
- session disappears while open
- session is visible but no longer controllable

## Testing Plan

### Unit tests in `OnibiCoreTests`

Add coverage for:

- websocket message decoding/encoding
- reconnect backoff behavior
- session delta merging
- transcript append and truncation banner state
- input frame construction for text and special keys

### iPhone view model tests

Add tests for:

- opening a session triggers buffer request and subscription
- incoming output frames append to transcript
- disconnect changes visible state
- session removal while open shows terminal unavailable state

### Manual QA checklist

1. Pair the iPhone app against a Phase 01-capable host.
2. Confirm sessions list loads after bootstrap.
3. Open a live session and verify the initial buffer appears.
4. Run a command from the phone and confirm live output arrives.
5. Send `Ctrl-C` while a long-running command is active.
6. Use arrow keys to move through shell history or cursor position.
7. Background the app and foreground it again; confirm reconnect.
8. Rotate the host token and confirm the app surfaces re-pairing needs.

## Acceptance Criteria

This phase is complete when:

- the iPhone app can list controllable sessions from the new host API
- the iPhone app can open a live session and show buffered plus live output
- the iPhone app can send text, `Enter`, `Ctrl-C`, arrow keys, and space
- the app reconnects cleanly when foregrounded after transient network loss
- the UI remains intentionally simple and clearly status-driven

## Risks And Tradeoffs

### Do not overbuild terminal UI

This phase is about reliable remote control, not terminal emulation. Avoid ANSI parsing, alternate-screen logic, and custom keyboard complexity unless a bug forces it.

### Keep transport in shared code

Do not let the iPhone app invent a client contract that the web app later cannot reuse.

### Realtime reliability matters more than polish

If there is a tradeoff between richer visuals and a simpler reconnect model, choose the simpler reconnect model.
