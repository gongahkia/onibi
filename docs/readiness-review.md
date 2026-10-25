# Readiness and recovery review

Date: 2026-10-25. Baseline: `5d771134`. Follow-up to the [cockpit review](cockpit-review.md). This implementation covers the agreed first code milestone: Mac diagnostics, honest phone recovery states, lower native polling pressure, and integration tests against the actual web server. The product remains **Control Ghostty wherever text renders**, controlling existing native identities.

## Principles and implementation

| Principle | Gap at baseline | Implemented behavior |
| --- | --- | --- |
| Explain what the Mac can do | Telegram-oriented diagnostics did not explain Ghostty installation, process, permissions, or version | `onibi web doctor` defaults to passive metadata/trust inspection, with structured JSON and separately requested inventory/capture probes |
| Keep status honest | One connection boolean conflated transport, native inventory, and output capture | Distinct observations and phone labels; capture failure alone does not disable targeted input, and capture/HTTP success cannot erase failed inventory evidence |
| Preserve the destination | Older observations and canceled inventory reads could complicate recovery | Completion ordering guards, detached canceled flights, per-pane capture state, and fresh native validation before every mutation |
| Make recovery specific | A generic refresh did not say which capability needed attention | Readiness sheet, permission explanations, retry-output action, explicit focus action, bounded retry delay, and visible session expiry |
| Keep repeated keys responsive | Every accepted key triggered additional inventory plus capture | Fresh server preconditions retained; redundant post-input inventory removed and post-input capture debounced by 400 ms |
| Back off when the app is struggling | Repeated clients could queue redundant inventory reads during failure | Shared in-flight inventory, last-waiter cancellation, and capped exponential failure backoff; no successful-inventory cache authorizes actions |
| Verify delivery recovery at the real boundary | Browser behavior used an independently implemented Node HTTP fixture | Additional Go HTTPS fixture uses production auth, embedded assets, SQLite receipts, and a simulated Ghostty backend, in Chromium and WebKit |
| Preserve explicit decisions | Longer-lived devices and stored drafts need retention/revocation choices | Existing twelve-hour sessions, restart revocation, and memory-only drafts remain; these policies now have end-to-end automated coverage |

Known Ghostty versions gate native controls at 1.3.0 and title actions at 1.3.1. This follows the [AppleScript API](https://ghostty.org/docs/features/applescript) and [1.3.1 release notes](https://ghostty.org/docs/install/release-notes/1-3-1), alongside the installed scripting dictionary and the bridge fixtures. Unknown/development versions remain unknown. Version metadata does not establish actual native responsiveness.

The readiness design applies [Apple's feedback guidance](https://developer.apple.com/design/human-interface-guidelines/feedback) through visible state and a next action. It preserves the [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve) private connection model and adds no public hosting or new transport.

## Full interface review

Mode: **full** within this pass's changed scope: connection status, readiness sheet, inventory/capture recovery, delivery labels, version-disabled controls, and session-expiry return to pairing. Stack: embedded HTML, plain JavaScript, existing plain CSS. The prior cockpit review covers the broader controls and terminal layout. Native Ghostty interaction and a physical iPhone remain outside this validation pass.

| Category | Evidence inspected | Result |
| --- | --- | --- |
| Typography | Readiness labels/details, 320px/390px sheets, session expiry and inventory timing | Human-readable check names; wrapped status; tabular numbers for changing values |
| Surfaces | Status button, inline recovery strip, readiness dialog, controls drawer | 44px controls, visible keyboard focus, sticky sheet exit, matching existing surface tokens |
| Animations | Existing explicit-property transitions and reduced-motion browser checks | No new animation; status and polling changes use static labels. No new timeline required a slowed-motion inspection |
| Icons | Existing status dot and labeled close button | Decorative dot hidden from assistive technology; meaning remains in text; no new icon system |
| Performance | Inventory concurrency/cancellation, failure scheduling, post-input observation, browser errors | Shared bounded work, capped retries, fewer reads during key entry, no runtime dependency added |

### Findings addressed

| Severity | Location | Before | After | Why |
| --- | --- | --- | --- | --- |
| HIGH | `internal/web/assets/app.js` — `setNetwork`, `setNative`, `readSnapshot` | One connection state could misrepresent native/capture availability | Independent state with ordering guards and distinct labels | Feedback must reflect the capability the user is about to use |
| HIGH | `internal/web/assets/app.js` — `applyReceipt` | A rejected action could leave stale readiness until the next poll | Reconcile inventory immediately, preserving the draft and explicit destination | Stop presenting stale controls after a failed precondition |
| MEDIUM | `internal/web/assets/index.html` — `health-dialog`; `app.js` — `loadHealth` | No phone explanation for permissions, version, or expiry | Named readiness checks, native timing, session expiry, and a refresh action | Expose useful recovery information at the point of failure |
| MEDIUM | `internal/web/assets/app.js` — `renderReadiness`, `retry-connection` | Generic retry targeted inventory even for capture-only failure | Retry output; focus remains a separate explicit action | Match the recovery action to the failing capability |
| MEDIUM | `internal/web/assets/app.js` — `readSnapshot` | A retained snapshot showed the same capture error in adjacent alerts | Recovery strip owns the explanation; retained output stays marked stale | Remove repeated feedback while keeping the problem and next action visible |
| MEDIUM | `internal/web/assets/app.js` — `updateControls` | Title actions appeared available on known 1.3.0 versions | Disabled controls with a version explanation; server/native guards too | Prevent predictably unsupported actions without hiding their purpose |
| MEDIUM | `internal/web/assets/app.js` — `applyReceipt`, `setNetwork`, `setNative` | Generic Done label, repeated post-key inventory, ambiguous Follow state after transport failure | Input/key delivery and close-request labels; debounced capture; Follow stops on native/transport failure | Precise feedback and lower repeated interaction cost |
| LOW | `internal/web/assets/style.css` — readiness/health rules | New details needed consistent narrow-screen presentation | Wrapped status, readable permission names, semantic colors plus text, tabular values, sticky close heading | Preserve touch targets and continuity with the existing cockpit |

### Considered but rejected

| Location | Candidate | Rejected because |
| --- | --- | --- |
| Readiness | Automatically focus or restart Ghostty to prove recovery | Would change existing Mac work without an explicit action |
| Failed inventory | Reuse the last successful list for mutation preconditions | The target can disappear or change while cached; action validation stays fresh |
| Phone session | Persist credentials/devices longer and save drafts | Device lifetime/revocation and draft retention require a product decision; current defaults remain intact |
| Status feedback | Animated reconnect loops or a streaming performance chart | Historical timings and static labels meet this scope without recurring motion or another polling stream |

### Verification and verdict

- `GOMAXPROCS=2 go test -race -p 2 ./internal/ghostty ./internal/web ./internal/cli` passed. Additional inventory coverage verifies a new client does not inherit abandoned work and a late abandoned result cannot overwrite fresh readiness.
- `go vet` passed for the changed packages and the tagged browser fixture. CGO-disabled Ghostty/web tests and the native CLI build passed.
- `npm test --prefix web-tests` with local Chrome and `ONIBI_BROWSER=webkit npm test --prefix web-tests` passed: seven JXA VM tests, phone UI/state checks, axe WCAG A/AA checks, and real Go HTTPS integration. Both engines run in CI.
- UI checks cover 320px/390px/desktop widths, landscape sheets, 44px touch controls, focus, loading, empty/error recovery, permission failures, preserved drafts, reduced motion, and readiness accessibility. Screenshots of readiness and capture-failure states were visually inspected.
- Real-server checks cover Secure/HttpOnly/SameSite cookies, single-use pairing, CSRF/origin, literal input, lost-response recovery, deduplication, native/capture independence, failure backoff, version guards, create/close reconciliation, crash during execution, restart revocation, and twelve-hour expiry. Test setup uses stdin and an isolated temporary database; it adds no HTTP testing route or production mode.
- Passive local diagnostics found Ghostty 1.3.1, one process, and Accessibility authorization. **Not verified:** Automation responsiveness, actual terminal input/output or focus, physical iPhone keyboard/VoiceOver, and the real Tailscale route. The earlier native timeout remains unresolved; no Ghostty restart or native probe was performed in this pass. TLS certificate bypass is confined to the ephemeral test server.

**Verdict: Approve** the changed interface and automated implementation scope, with the native/device/Tailscale checks above explicitly unverified. CI links and the exact tested head are reported at handoff, rather than claiming automated tests establish native success.

## Remaining decisions and validation

1. Validate the existing native path in a disposable Ghostty workspace when the application responds. Measure input-to-output latency and iPhone keyboard friction. This is still the largest evidence gap.
2. Agree on device enrollment, renewal and revocation before adding unattended background web-service installation. Today a fresh pairing link requires a web-process restart, which revokes all browser sessions.
3. Choose whether any draft persistence is wanted, and its retention/clearing rules. Current drafts remain page-memory only.
4. Reader preferences, richer terminal observation and semantic agent status remain future scope. They should preserve exact native identity and state their API limitations, rather than infer agent state from terminal titles.
