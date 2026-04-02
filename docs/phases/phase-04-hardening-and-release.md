# Phase 04: Hardening, Diagnostics, And Release Readiness

## Objective

Stabilize the new remote-control system so it is safe enough to use daily as a personal paired-device product.

At the end of this phase, the system should be operationally understandable, diagnosable, and resilient enough for real usage across:

- the macOS host
- the native iPhone app
- the web client

This phase is where reliability and operational discipline matter more than feature expansion.

## Non-Negotiable Constraints

- Do not edit `README.md`.
- Do not expand the v1 scope into a full remote terminal product.
- Do not introduce a cloud account system.
- Preserve the paired-device Tailscale + token model.
- Prefer kill switches, diagnostics, and clear failure states over hidden automation.

## Scope Summary

This phase should harden the outputs of Phases 01-03.

That includes:

- connection resilience
- stale-session cleanup
- token rotation behavior
- diagnostics visibility
- feature flagging and kill switches
- QA coverage
- release documentation inside the codebase, not in `README.md`

## Current Risk Areas To Address

The earlier phases introduce several new operational risks:

1. host and proxy version mismatch
2. stale websocket clients
3. stale proxy registrations after terminal exit or host sleep
4. unbounded output growth
5. confusing behavior after token rotation
6. shell bootstrap regressions
7. web static assets drifting out of sync with host API behavior

This phase exists to remove or sharply reduce those risks.

## Recommended Implementation Areas

## Area 1: Feature flags and kill switches

Add explicit settings fields to the existing macOS settings model for:

- enabling or disabling remote control globally
- enabling or disabling the served web app separately if needed
- buffer size limits
- proxy inactivity timeout

Recommended flags:

- `remoteControlEnabled`
- `webClientEnabled`
- `realtimeGatewayEnabled`

The host should refuse remote input when the feature is disabled, even if older clients still attempt to connect.

## Area 2: Version compatibility and protocol negotiation

Add a simple compatibility check between:

- host app
- session proxy
- client realtime protocol version

Requirements:

- the websocket `auth_ok` frame should include protocol and host version
- proxy registration should include proxy version
- mismatches should be surfaced in diagnostics and not fail silently

Recommended behavior:

- compatible minor mismatch -> warn only
- incompatible major mismatch -> reject with explicit error

## Area 3: Diagnostics and observability

Extend diagnostics on the host and clients.

### Host diagnostics

Add or extend visibility for:

- active controllable session count
- controllable session count by status
- connected realtime client count
- proxy connection failures
- proxy disconnect count
- websocket auth failures
- last token rotation time
- session buffer truncation count
- last session input error

Recommended file targets:

- `Onibi/Services/DiagnosticsStore.swift`
- `Onibi/Services/MobileGatewayService.swift`
- `Onibi/Views/SettingsView.swift`
- `OnibiCore/Models/MobileAPIModels.swift`

### iPhone diagnostics

Expose lightweight client-facing states:

- last realtime connection time
- current websocket state
- last auth error
- last session transport error

### Web diagnostics

Expose lightweight debug information in a non-primary settings panel or footer:

- websocket state
- last reconnect time
- host version
- protocol version

Do not clutter the main session UI with debug noise.

## Area 4: Session lifecycle cleanup

Implement aggressive but understandable cleanup rules.

### Required cleanup behavior

- mark a session stale if proxy heartbeat is missing beyond the timeout
- remove fully exited sessions after a grace period unless the client is still viewing buffered output
- close websocket subscriptions for removed sessions cleanly
- purge old output buffers for expired sessions

Recommended timeout defaults:

- heartbeat timeout: 30-60 seconds
- exited session grace period: 5 minutes

Document the chosen values in code comments and tests.

## Area 5: Token rotation behavior

The current product already has pairing token rotation. The remote-control system must behave correctly when the token changes.

Required behavior:

- new HTTP requests with old token fail with `401`
- new websocket auth with old token fails explicitly
- existing live websocket sessions authenticated with the old token should be closed on rotation
- clients should surface a clear re-pairing requirement rather than looping forever

Add targeted tests for this flow.

## Area 6: Shell bootstrap and safe fallback

If the session proxy bootstrap fails, the user's shell should fail open rather than leaving them without a terminal.

Recommended behavior:

1. attempt proxy bootstrap
2. if bootstrap fails quickly, log diagnostics
3. exec into the original shell directly
4. mark the session as monitor-only instead of controllable

This is an important product behavior. A failed remote-control feature must not break local Ghostty use.

## Area 7: Build and asset synchronization

The web app and host app must not silently drift.

Required safeguards:

- the host should expose the served web build version in diagnostics
- the web app should display host version and protocol version somewhere non-primary
- if the host cannot find bundled assets, diagnostics should expose it clearly

If build scripts are added, they should be deterministic and documented in code-local comments or scripts, not hidden manual steps.

## Recommended Test Matrix

## Automated tests

### Host-side

Add tests for:

- stale session cleanup
- proxy version mismatch handling
- token rotation invalidates existing websocket sessions
- shell bootstrap fail-open path
- diagnostics counters update as expected

### iPhone-side

Add tests for:

- unauthorized state after token rotation
- reconnect stops after explicit unauthorized response
- session removed while open

### Web-side

Add tests for:

- unauthorized bootstrap handling
- websocket auth rejection handling
- reconnect behavior on transient network failure vs token failure

## Manual QA matrix

Run at minimum:

1. macOS host + iPhone app over Tailscale
2. macOS host + mobile Safari web app over Tailscale
3. macOS host + Android Chrome web app over Tailscale
4. macOS host sleep/wake while a session is open remotely
5. token rotation while both iPhone and web client are connected
6. proxy bootstrap disabled or broken, ensuring local Ghostty still works

## Release Checklist

The implementation agent should leave this phase with a release checklist committed into the repo under `docs/` or a similarly local path, but still should not edit `README.md`.

The checklist should cover:

- shell hook migration verified
- remote control kill switch verified
- token rotation verified
- diagnostics visible in host UI
- iPhone reconnect and unauthorized behavior verified
- web reconnect and unauthorized behavior verified
- Tailscale Serve configuration path verified

## Acceptance Criteria

This phase is complete when:

- remote control can be disabled cleanly from the host
- diagnostics are sufficient to debug pairing, proxy, websocket, and buffer failures
- token rotation behaves predictably across host, iPhone, and web
- stale sessions and buffers are cleaned up automatically
- shell bootstrap failures do not break normal Ghostty usage
- the system is reliable enough for day-to-day paired-device use

## Explicitly Deferred Beyond Phase 04

Do **not** pull these into v1 unless a blocking bug forces it:

- full ANSI terminal emulation
- alternate-screen app support
- mouse support
- browser push notifications
- native APNs push pipeline
- Linux or Windows host support
- multi-user accounts or cloud relay architecture

## Final Guidance For The Implementing Agent

When there is a choice between adding another feature and making failure modes legible, choose legibility.

This product is only viable if the user can tell, with minimal guesswork, whether a problem is:

- pairing
- Tailscale reachability
- websocket auth
- proxy bootstrap
- session expiry
- token rotation

Design the diagnostics and kill switches accordingly.
