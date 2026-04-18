# Onibi Active TODO

Status date: April 18, 2026

This file tracks only work that is still open. Completed/scaffolded items have been removed.

## Current Repo Reality

- Core host control-plane code exists (`Onibi`, `OnibiCore`, `OnibiSessionProxy`).
- Web companion app exists and is integrated (`OnibiWeb`, bundled into `Onibi/Resources/OnibiWeb`).
- Automated tests currently pass locally:
  - `swift test` (263 tests)
  - `npm --prefix OnibiWeb test` (26 tests)

## Priority 0: Phase 01 Exit (Manual Reality Validation)

These checks must be performed on a real macOS + Ghostty runtime before calling Phase 01 complete.

### 1. Host startup and feature toggles

- Build and run host:

```bash
cd /Users/gongahkia/Desktop/coding/projects/onibi
swift build --product OnibiSessionProxy
swift run Onibi
```

- In `Settings -> Mobile Access`:
  - Enable mobile gateway
  - Enable remote control
  - Verify gateway and local proxy listener are both `Running`

### 2. One-off proxy smoke test (without shell-hook automation)

In a fresh Ghostty tab:

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

### 3. Gateway + session visibility check

```bash
export ONIBI_PAIRING_TOKEN='<paste token here>'

curl -s -H "Authorization: Bearer $ONIBI_PAIRING_TOKEN" \
  http://127.0.0.1:8787/api/v2/bootstrap

curl -s -H "Authorization: Bearer $ONIBI_PAIRING_TOKEN" \
  http://127.0.0.1:8787/api/v2/sessions
```

Expected: controllable session visible and remote-control feature flags enabled.

### 4. Realtime path + input path check

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

Expected: `auth_ok`, snapshots, `input_accepted`, and live output.

### 5. Special-key behavior on real shell

Validate all of these against a real running proxied shell:

- `ctrl_c`
- `arrow_up`, `arrow_down`, `arrow_left`, `arrow_right`
- `space`

### 6. Real shell-hook bootstrap path

- Install/update shell hooks (Settings UI or script).
- Fully restart Ghostty.
- Confirm new shells become controllable automatically without manual `exec OnibiSessionProxy`.

### 7. Failure-mode checks

- Invalid HTTP token -> `401`
- Invalid websocket token -> auth error + disconnect
- Disable remote control at runtime -> listener stops, behavior degrades safely
- Disable mobile gateway at runtime -> HTTP/websocket access stops
- Host restart behavior
- Proxy disconnect behavior
- Host sleep/wake behavior

## Priority 1: Engineering Gaps To Close

### 1. Token rotation semantics for existing websocket clients

Open requirement:

- Existing authenticated realtime clients should be force-closed when token rotates.

Current state:

- New auth attempts are re-validated, but existing clients are not clearly invalidated on rotation.

### 2. End-to-end proxy integration tests

Add high-value integration coverage for:

- proxy register -> session appears
- output frame -> buffer + realtime broadcast
- websocket `send_input` routing
- invalid websocket auth handling
- disconnected proxy -> stale session cleanup

### 3. Diagnostics expansion

Add explicit diagnostics counters/fields for:

- websocket auth failures
- proxy disconnect count
- token rotation timestamp visibility
- buffer truncation count
- last input routing error

### 4. Session lifecycle hardening tests

Automate checks for:

- stale/expired session cleanup
- behavior across host sleep/wake-like lifecycle conditions
- websocket subscription cleanup when sessions are removed

## Priority 2: Release Hardening (Phase 04-focused)

### 1. Fail-open shell bootstrap

If proxy bootstrap fails, local shell must still open and remain usable.

### 2. Version compatibility signaling

Make compatibility status explicit across:

- host
- proxy
- realtime protocol

### 3. Cross-device manual matrix

Run real manual matrix:

1. macOS host + mobile Safari over tunnel/Tailscale
2. macOS host + Android Chrome over tunnel/Tailscale
3. token rotation while live web session is connected
4. host sleep/wake during active remote session
5. broken proxy bootstrap while ensuring local shell still works

## Recommended Work Order (Now)

1. Finish Priority 0 manual validation end-to-end.
2. Fix defects found during manual validation immediately.
3. Implement Priority 1 item #1 (token rotation force-disconnect).
4. Add Priority 1 integration tests.
5. Complete remaining Phase 04 hardening items.

## Definition Of Done For Phase 01

Phase 01 is complete only when, on a real Ghostty-driven macOS setup:

- controllable sessions auto-appear via installed hooks
- `/api/v2/sessions` reflects them correctly
- realtime auth and streaming are stable
- remote text + key controls work in practice
- existing `v1` monitoring behavior remains intact
- failure modes are understandable and diagnosable
