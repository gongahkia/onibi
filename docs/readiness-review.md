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

## Follow-up decisions from the first milestone (superseded below)

1. Validate the existing native path in a disposable Ghostty workspace when the application responds. Measure input-to-output latency and iPhone keyboard friction. This is still the largest evidence gap.
2. Agree on device enrollment, renewal and revocation before adding unattended background web-service installation. Today a fresh pairing link requires a web-process restart, which revokes all browser sessions.
3. Choose whether any draft persistence is wanted, and its retention/clearing rules. Current drafts remain page-memory only.
4. Reader preferences, richer terminal observation and semantic agent status remain future scope. They should preserve exact native identity and state their API limitations, rather than infer agent state from terminal titles.


## Code hardening follow-up: recommendations 2–5

Baseline: `d3000ca8`. The user chose code implementation before manual native validation and explicitly approved optional **remember this browser for 30 days**, named devices, and individual revocation. This section supersedes the earlier enrollment/reader deferrals above. No README or public website was added. No Ghostty focus, input, capture, restart, service installation, or native probe was performed during this follow-up.

### Product and engineering results

| Recommendation | Result | Evidence and limit |
| --- | --- | --- |
| 2. Close safety | Confirmation carries exact reviewed pane IDs. HTTP preflight and the JXA bridge reject added, removed, or moved panes. Harmless title/order changes remain allowed. Successful dispatch gets at most three bounded observations without replay | Go race tests, JXA VM membership races, and both browsers against real HTTPS. Outcomes distinguish removed, still present, and unverified. Native checks and close remain separate OS calls; a local prompt or final-check race remains possible |
| 3. Pairing and operation | Fresh local links, named temporary/remembered devices, individual and local all-device revocation, remembered restart recovery, and a separate Mac web login service | Real SQLite/HTTPS restart, fixed-expiry, origin, CSRF, queued/preflight revocation tests; real local Unix socket tests; fake launchctl lifecycle tests. Live launchd/TCC and Tailscale remain unverified |
| 4. Phone reading | Browser-only text size, wrap, expanded view, snapshot navigation, stable copy/selection, and destination reconciliation | Chromium/WebKit long-snapshot, 320px/landscape, focus-return, copy/fallback, local-only effects, storage and axe checks. Physical iPhone clipboard/keyboard/VoiceOver remains unverified |
| 5. Capability evidence | Passive authenticated capability API and a grouped phone view separate implementation, automated contract coverage, version support, native observations, and unsupported scope | Contract coverage follows the validator and control catalog. Simulations never count as native verification; input acknowledgement never proves a program outcome. Native observations are historical and reset with the web process |

The remembered grant has a fixed 30-day deadline. The Mac stores a SHA-256 credential hash and display metadata, never the raw remembered secret. Temporary sessions remain memory-only and end on restart. A remembered GET session exchange issues a fresh CSRF token; an expired command is not retried. Access is checked before waiting, after waiting, and after native target validation. Revocation cannot recall an already dispatched OS call. `revoke-all` also invalidates an unused pairing link. Reader preferences are the only new local-storage data; drafts and captured output remain memory-only.

The web service has a separate `sh.onibi.web` label and logs, uses the graphical user domain and Aqua session, emits no pairing secret to background logs, and allows 45 seconds for shutdown. Install writes configuration; start is explicit, with automatic startup at subsequent login. Stop unloads it, and failed stop prevents configuration removal. Tests never invoke real launchctl. This follows [Apple’s launch-agent guidance](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html) and [Apple’s launchd property definitions](https://github.com/apple-oss-distributions/launchd/blob/main/man/launchd.plist.5). Clipboard success and fallback follow the browser’s [Clipboard.writeText contract](https://developer.mozilla.org/en-US/docs/Web/API/Clipboard/writeText); a physical mobile permission/gesture check is deferred.

### Full interface review of the follow-up

Mode: **full**. Scope: pairing choice, device list/revocation, close outcome feedback, reader controls/expanded view, and capability view. Framework/styling: existing embedded HTML, plain JavaScript and plain CSS; no new runtime dependency, transport, or design system. Existing controls and earlier reviews retain their stated boundaries.

| Category | Evidence inspected | Result |
| --- | --- | --- |
| Typography | Pairing copy; narrow reader at 22px; literal long lines; named-device expiries; capability details | Bounded reader sizes, optional wrapping, readable text labels, long metadata wraps |
| Surfaces | Pair card; device/revoke dialogs; expanded portrait/landscape reader; grouped capability sheet | Existing amber/mint surfaces, 44px controls, explicit destructive confirmation, visible reader exit, native dialog focus trapping/return |
| Animations | Existing press/color transitions; reduced-motion regressions; new controls | No new staged or recurring animation. Existing 0.96 press feedback retained. A 10%-speed animation-panel review was not performed |
| Icons | Existing close/read/status marks and new text-led controls | No new icon vocabulary; controls keep explicit accessible labels |
| Performance | Reader DOM movement; long snapshots; storage payload; passive capability API; receipt recovery | One output element, no duplicated stream, no native calls for reader actions/capability refresh, small preference-only storage, no replay on renewal |

| Severity | Location | Before | After | Why |
| --- | --- | --- | --- | --- |
| HIGH | `internal/web/assets/app.js:1079` close feedback | A native close acknowledgement could look like verified removal | Distinct confirmed/still-open/unverified feedback after bounded observation | State feedback must match available evidence |
| HIGH | `internal/web/assets/index.html:73`, `app.js:1641` pairing | Links immediately paired with one implicit lifetime | Explicit name and unchecked 30-day option; temporary default | Access duration is a user decision, visible before enrollment |
| MEDIUM | `internal/web/assets/index.html:718`, `app.js:1666` devices | No browser inventory or per-device recovery | Named active grants, expiries, current-browser marker, confirmed revocation, local recovery commands | Make access understandable and recoverable |
| MEDIUM | `internal/web/assets/reader.js:17`, `style.css:1493` reader tools | Fixed tiny text and horizontal scrolling only | 12–22px text, optional wrap and snapshot start/end; preference-only persistence | Improve reading without changing Mac display or terminal state |
| MEDIUM | `internal/web/assets/reader.js:55`, `style.css:1570`, `app.js:711` expanded reading | Long output competed with controls; no isolated reading view | One output element in a native dialog, named destination, visible Done, focus return, dismissal on lost destination | Maintain context and usable touch/keyboard navigation |
| MEDIUM | `internal/web/assets/reader.js:70` copy/selection | No deliberate copy flow or stable selection during Follow | Literal copy, honest selected-text fallback, Follow pauses during selection | Clipboard uncertainty must have a usable fallback and static feedback |
| MEDIUM | `internal/web/assets/index.html:676`, `capabilities.js:1`, `style.css:1624` capability view | Support boundaries lived mainly in documentation | Grouped feature details with coverage, requirements and conservative native evidence | Expose what users can rely on without implying agent semantics or full emulation |

### Considered but rejected in this follow-up

| Location | Candidate | Rejected because |
| --- | --- | --- |
| Reader | Use Mac font/scroll controls for phone readability | Those mutate the actual desktop; phone presentation needs independent controls |
| Reader | Add a terminal emulator and streaming transport | Current capture is bounded Accessibility plaintext, so such a UI would imply unsupported fidelity |
| Pairing | Silently remember every browser | The approved policy is explicit opt-in with fixed expiry and revocation |
| Capability view | Infer agent progress from pane titles or mark delivery as task success | Neither is reliable evidence of agent state or program completion |
| Verification | Launch/restart Ghostty or install the service to finish testing | The user deferred native/manual testing; fakes and real HTTP/SQLite boundaries cover the authorized code work |

### Verification and remaining boundary

- `GOMAXPROCS=2 go test -race -p 2 ./internal/ghostty ./internal/web ./internal/webadmin ./internal/service ./internal/cli`: changed packages use real temporary SQLite and sockets with simulated native/service backends. Close membership/outcome, remembered expiry/restart/revocation, slow-preflight revocation and lifecycle failure paths are covered.
- `ONIBI_CHROME_PATH='/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' npm test --prefix web-tests` and `ONIBI_BROWSER=webkit npm test --prefix web-tests`: eight JXA VM tests, UI regressions and production Go HTTPS integration. Targeted UI and integration runs were also used per atomic change. New device/capability and expanded-reader surfaces pass automated axe checks. Portrait and landscape reader screenshots, pairing, device and capability screens were inspected.
- Native Ghostty, physical iPhone, VoiceOver/keyboard, real clipboard permissions, live launchd/TCC, and the actual Tailscale route are **not verified**. The earlier native timeout is not diagnosed or resolved by these code changes. No semantic agent integration or full terminal emulation is claimed.

**Verdict: Approve** the implemented code and reviewed interface scope, subject to the explicit unverified native/device checks above. The exact final GitHub Actions head and result are reported in the handoff; automated coverage is not native acceptance.

## Monochrome cockpit and source lifecycle review

Mode: **full**. Scope: embedded web typography, palette, branding and pairing density; foreground CLI startup/status/logs; build/install/update/uninstall presentation and recovery. Styling: existing HTML/JavaScript/plain CSS, with local JetBrains Mono WOFF2 faces. Terminal presentation: a small Go line-oriented renderer; no full-screen framework, Rust runtime or new application dependency. The user selected source-based updates and deferred native/manual testing.

### Findings and implementation

| Severity | Location | Before | After | Why |
| --- | --- | --- | --- | --- |
| MEDIUM | `internal/web/assets/style.css`, `index.html` | Amber/mint palette, repeated brand graphics and mixed fonts | Neutral surfaces, text wordmark, local JetBrains Mono throughout, restrained borders | Matches the requested minimalist direction and avoids external font requests |
| MEDIUM | `style.css` mobile pairing | Large introductory area placed the primary action below a phone viewport | Compact phone introduction and spacing; connection action fits the tested 390×844 viewport | Make initial pairing usable before decorative content consumes the screen |
| MEDIUM | `internal/termui`, `internal/devtool/build.go` | Silent raw build and an older shell loader | Go-port Rattles frames, real stage/package activity, elapsed time, private compiler log and explicit failure output | Show liveness without inventing completion percentages; preserve readable output in pipes |
| HIGH | `internal/devtool/install.go` | No owned source update/removal workflow | Manifest/checksum ownership, previews, clean-main optional fast-forward, staged replacements and rollback | Protect unrelated files and keep a failed update recoverable |
| HIGH | `internal/devtool/build.go` | Build artifacts could change between publication and installation copy | Build lock spans the copy; manifest uses the build's captured revision | Installed provenance must match the binaries actually copied |
| HIGH | `internal/service/launchd.go`, `systemd.go` | Telegram service removal could ignore stop failure | Stop failure retains configuration and fails removal | The new uninstaller must not remove executable files after silently failing to stop their service |
| MEDIUM | `internal/cli/web.go`, `web_observability.go` | Startup text lacked runtime inspection and event guidance | ASCII startup panel, exact executable hints, cached status, version JSON, bounded log tail/follow | Make local operation and diagnosis possible without a native probe |
| HIGH | `internal/web/observability.go`, `internal/logging` | No content-safe web event stream or continuous runtime rotation | Allowlisted metadata, request correlation, deduplicated outcome counts, private rotated JSON, readable console | Provide operational evidence without storing terminal content or access secrets |
| MEDIUM | `Makefile`, CI | Generic run/setup paths led to Telegram; default tests invoked live E2E | Web defaults, explicit Telegram targets, hermetic default tests, real isolated lifecycle CI on Mac/Linux | Align first-run paths with this product and keep live tests deliberate |

### Coverage

| Category | Review evidence and result |
| --- | --- |
| Typography | Regular and semibold local JetBrains Mono are loaded in Chromium/WebKit; font readiness and no external requests are asserted. Terminal output preserves literal text, sizing and independent phone reader preferences. |
| Surfaces | Pairing, desktop cockpit, mobile terminal and existing dialogs retain visible labels/borders and 44px controls. Neutral primary/secondary actions and explicit danger copy carry meaning without hue. Pairing density was corrected after screenshot inspection. |
| Animations | Existing short press/color feedback and reduced-motion handling remain. No new browser loops or entrance animation. Terminal spinners run only while work is active, clean up on completion/cancellation/panic, and disappear from redirected/CI/plain output. No 10%-speed animation-panel review was performed. |
| Icons | Removed the decorative ghost illustration and redundant logo; kept existing functional controls with accessible labels. No new icon vocabulary. Terminal block art appears once, with compact/plain fallbacks. |
| Performance | Two embedded WOFF2 faces, no CDN, no additional runtime JS/dependency. Cached status performs no Ghostty calls. Routine successful polling is debug-only; runtime logs and reader memory are bounded. Build process cancellation includes compiler descendants. |

### Considered and rejected

- A full-screen terminal dashboard: it would hide normal scrollback and complicate piping and interruption for a server whose main job is to stay running.
- A fake percentage for Go compilation: package work and cache hits vary; real stages, elapsed time and package names provide honest progress.
- Automatic service restarts during updates: the agreed policy preserves running work until an explicit restart request.
- Packaged release downloads in this pass: the user chose the source workflow; provenance and safe replacement were implemented for that path first.
- Colored status accents or a new mascot: the requested visual direction is monochrome with less branding; text, borders and existing labels distinguish states.

### Verification and limits

- Race tests cover request correlation/content exclusion at debug level, replay-safe counters, local status sockets, runtime rotation, tail/follow through rotation, cancellation, unsafe paths, ownership checks, failed builds/replacements, pull guards and service removal failures.
- The opt-in `ONIBI_SOURCE_SMOKE=1 go test -run TestSourceLifecycleIntegration -v ./internal/devtool` compiles real artifacts, installs/updates into an isolated path containing spaces, checks binary/manifest provenance and previews, uninstalls, and verifies unrelated files/state remain. It runs in both Mac/Linux CI jobs and invokes no native services or terminal controls.
- Local verification includes `go vet ./...`, full `go test -race ./...`, Chromium/WebKit UI and production HTTPS/SQLite integration suites, axe checks, font-load assertions, and reviewed mobile/desktop screenshots. A native `make build` exercises real TTY progress without starting the application. The exact final CI head/result is recorded in the handoff.
- Real Ghostty input/capture, physical iPhone/Safari and VoiceOver, live launchd/TCC, and the actual Tailscale route remain unverified. This pass neither restarts Ghostty nor resolves the earlier native automation timeout. Rotation is bounded local diagnostics, not guaranteed archival event delivery; atomic replacements are per file with rollback, not power-loss-safe multi-file transactions.

Verdict: implemented scope is ready for the deferred manual acceptance checks once automated CI is green. Native reliability remains an explicit open validation item.
