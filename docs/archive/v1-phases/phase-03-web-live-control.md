# Phase 03: Cross-Platform Web Client

## Objective

Create a first-class web client for Onibi that uses the host API and works on mobile and desktop browsers over the user's Tailscale-served host URL.

At the end of this phase, the web app must be able to:

- connect to the same host URL and pairing token used by the host
- list controllable sessions
- open a live session
- render buffered and live output
- send text, `Enter`, `Ctrl-C`, arrow keys, and space
- work well on mobile browsers and desktop browsers

This phase should favor simplicity, directness, and operational reliability over visual experimentation.

## Non-Negotiable Constraints

- Do not edit `README.md`.
- The web client must use the same host API and message contract introduced in Phase 01.
- Keep the visual design intentionally simple and aligned with Onibi's existing host settings views.
- Do not require a cloud backend.
- The web app should be served by the user's own Onibi host over Tailscale, not by a separate hosted service.
- Live behavior only needs to work while the browser tab is open.

## Current Repo Context

This repo currently includes a web frontend and a Swift host with:

- `Onibi/` macOS host app
- `OnibiCore/` shared Swift models/services
- `OnibiWeb/` companion web app
- `Package.swift`

This phase focuses on continuing the web client as the primary companion experience.

## Recommended Web Stack

Use a small React + TypeScript + Vite app.

### Why this stack

- minimal setup overhead
- good mobile and desktop support
- straightforward websocket handling
- fast local development
- easy static build output for bundling into the host app

Recommended new directory:

- `OnibiWeb/`

Recommended initial contents:

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

## Serving Model

The macOS host should serve the web app itself.

### Production serving path

Recommended route behavior on the host:

- `GET /` -> web app index
- `GET /assets/*` -> static JS/CSS assets
- `GET /api/v2/*` -> host API
- `GET /api/v2/realtime` -> websocket endpoint

### Asset bundling approach

Choose one of these and keep the implementation explicit in the commit:

1. build the web app into a folder bundled with the macOS host app resources
2. build into a known local filesystem path and have the host serve from there in development, then bundle for release

The preferred production direction is bundled static assets. The implementation agent should make that concrete in code and project configuration rather than leaving it implicit.

## Authentication Model

Match the paired-device model, not a cloud login flow.

### Browser-side stored values

Store locally in browser storage:

- `baseURL`
- `pairingToken`

These are user-provided values from the host app, so the web app should mirror that flow.

### Connection flow

1. User enters base URL and pairing token.
2. Web app calls `GET /api/v2/bootstrap` with `Authorization: Bearer <token>`.
3. If bootstrap succeeds, save credentials locally.
4. Open websocket connection to `/api/v2/realtime`.
5. Send auth frame as the first websocket message.

Do not use cookies or a multi-account model.

## UI Requirements

## General layout

Keep the app mobile-first and uncluttered.

Recommended routes:

- `/connect`
- `/sessions`
- `/sessions/:id`

### Required screens

#### Connection screen

Fields:

- host URL
- pairing token

Actions:

- connect
- clear saved connection

States:

- first-time setup
- saved credentials present
- unauthorized
- unreachable

#### Sessions screen

Must show:

- controllable sessions list
- session name
- live/disconnected status
- last activity
- last command preview if available

Keep filtering and controls minimal. No search is required in v1.

#### Live session screen

Must show:

- session header with state and activity
- output pane
- bottom input bar
- special key strip

## Interaction Design

### Text input

The bottom composer should allow the user to type a line of text and submit it.

### Special keys

Visible controls are sufficient for v1:

- `Enter`
- `Ctrl-C`
- `Up`
- `Down`
- `Left`
- `Right`

Space can be handled through the text input itself. Add a dedicated space button only if it helps on narrow mobile keyboards.

### Output behavior

Requirements:

- monospaced font
- preserve line breaks
- append new output without full-page rerenders
- auto-scroll to bottom by default
- stop auto-scroll if the user manually scrolls upward
- allow the user to jump back to live output

Do not attempt full ANSI terminal rendering unless it is trivial and stable.

## Shared Contract Reuse

This phase must not invent a web-only API.

The web app must use the same host message contract:

- bootstrap via HTTP
- realtime auth via websocket frame
- session subscribe/unsubscribe
- request buffer
- send text input
- send key input
- receive session deltas and output chunks

If the implementation agent discovers a contract mismatch with Phase 02, Phase 02 should be corrected rather than forking the web contract.

## Recommended Implementation Tasks

## Task 1: Scaffold `OnibiWeb/`

Create the web project with the minimal tooling needed for build, local dev, and static asset generation.

Required scripts:

- `dev`
- `build`
- `preview`

## Task 2: Implement API client and realtime client

Create a small typed API layer with:

- bootstrap request
- sessions request
- buffer request
- websocket lifecycle manager
- typed send/receive helpers

Keep transport logic separate from React components.

## Task 3: Implement local session store

Create a small store for:

- connection state
- session list
- selected session details
- transcript chunks
- websocket lifecycle state
- reconnect behavior

Do not overuse global state. Keep the store intentionally small.

## Task 4: Build the three required routes

Implement:

- connection page
- sessions page
- live session page

Use a simple router. React Router is acceptable if needed; a tiny custom router is also acceptable if it remains maintainable.

## Task 5: Build responsive layout and controls

The app must work on:

- iOS Safari-sized viewports
- Android Chrome-sized viewports
- desktop browsers

Do not create separate desktop and mobile products. Use one responsive layout.

## Task 6: Serve static assets from the macOS host

Update the host gateway so the built web app is reachable from the same Tailscale base URL.

Implementation requirements:

- route `/` to the web app
- serve hashed JS/CSS assets
- do not break existing `/api/` routes
- return the app shell for known client-side routes if using SPA routing

## Task 7: Validate end-to-end mobile-browser usage

The web app must be manually tested from a phone browser against a real Tailscale-served host.

## Testing Plan

### Web unit tests

If a test runner is introduced, add coverage for:

- realtime message decoding
- session store merge logic
- reconnect behavior
- transcript append logic
- input frame creation

### Host integration tests

Add or extend tests to verify:

- the host serves the web app index
- static asset routes work
- API routes remain unaffected
- websocket realtime path remains functional

### Manual QA checklist

1. Build the web app and confirm the macOS host serves it.
2. Open the served app from a desktop browser and pair successfully.
3. Open the served app from a phone browser and pair successfully.
4. List controllable sessions.
5. Open a live session and verify buffered output appears.
6. Send text and confirm command execution.
7. Send `Ctrl-C` during a long-running command.
8. Use arrow keys and confirm shell movement/history behavior.
9. Refresh the browser and confirm the app reconnects cleanly.

## Acceptance Criteria

This phase is complete when:

- the web app is served by the user's own Onibi host
- the web app can pair with base URL + token and store them locally
- the web app can list live sessions and open one
- the web app can show buffered and live output
- the web app can send text plus the required v1 key set
- the UI works on mobile and desktop browsers without becoming overly complex

## Risks And Tradeoffs

### Serving from the host is the right constraint

Avoid splitting deployment into a separate web service. That would fight the personal paired-device model and add operational complexity.

### Browser websocket auth needs care

Because browsers cannot rely on arbitrary websocket headers, keep auth in the first websocket frame and ensure the server rejects unauthenticated frames.

### Keep the web app modest

Do not let this phase drift into offline caching, push notifications, PWA polish, or terminal emulation. Those are later concerns.
