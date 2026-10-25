# Ghostty cockpit: capability and interface review

Review date: 2026-10-25. Baseline: `030128cf`. Product: **Control Ghostty wherever text renders.** This pass stays within the existing Mac-served web interface, native Ghostty adapter, and private Tailscale connection. Telegram remains an optional, separate managed-session surface.

Follow-up: the [readiness and recovery review](readiness-review.md) covers passive Mac diagnostics, capability/version checks, failure backoff, independent phone status, and browser integration against the real Go HTTPS server. The findings below describe the original cockpit pass.

## Most useful next step

Make the existing Ghostty hierarchy usable from the phone: choose the right pane, understand the last observed output, send an exact input or key, and organize the native workspace. This benefits an agent, a shell, an editor, a REPL, and a log viewer without requiring a separate integration for each program.

The prior preview had explicit pane selection, input, a handful of keys, snapshots, and tab creation/closure. The highest-impact gaps were missing native workspace controls, a composer shared across destinations, and manual-only observation. This pass adds those controls while preserving exact identities and uncertain-delivery recovery.

## Research and product decisions

- [Ghostty's AppleScript documentation](https://ghostty.org/docs/features/applescript) and the [1.3.1 scripting dictionary](https://github.com/ghostty-org/ghostty/blob/v1.3.1/macos/Ghostty.sdef) describe windows, tabs, and terminal objects. The installed app is 1.3.1. We inspected its local dictionary and the matching native implementation, rather than assuming features from a development build.
- [Ghostty's action reference](https://ghostty.org/docs/config/keybind/reference) informs the bounded layout, scrollback, and display controls. The [1.3.1 action definitions](https://github.com/ghostty-org/ghostty/blob/v1.3.1/src/input/Binding.zig) confirm their names for this installation. The HTTP API accepts control IDs, not arbitrary action strings.
- [Ghostty shell integration](https://ghostty.org/docs/features/shell-integration) supplies useful titles and prompt navigation. It does not establish a dependable agent task/approval state for this native interface. Onibi shows titles and folders without guessing whether an agent is working or waiting.
- [Blink's smart keys](https://docs.blink.sh/#smart-keys) demonstrate the importance of modifiers and navigation on a phone. Onibi puts frequent keys beside the composer and the larger vocabulary in a named-destination drawer. It retains explicit Send versus Paste.
- [Apple's button guidance](https://developer.apple.com/design/human-interface-guidelines/buttons), [accessibility guidance](https://developer.apple.com/design/human-interface-guidelines/accessibility), and [motion guidance](https://developer.apple.com/design/human-interface-guidelines/motion) inform touch targets, clear labels, restrained press feedback, and reduced motion. [WebKit's safe-area guidance](https://webkit.org/blog/7929/designing-websites-for-iphone-x/) informs the edge padding. The page scrolls naturally; it does not depend on fragile keyboard-position calculations.
- [Playwright's accessibility guidance](https://playwright.dev/docs/accessibility-testing) informs the automated axe checks. These are useful regressions, not a claim of full accessibility conformance or physical iPhone validation.

The warm amber/mint palette, quiet terminal surface, small ghost illustration, and conversational copy are design choices for this project. They make the cockpit more welcoming without placing animation or decoration over terminal output.

## Capability map

| Work on the Mac | Phone capability now | Boundary |
| --- | --- | --- |
| Find existing work | Search grouped windows, tabs, and panes by title or directory; show the focused pane | No inferred agent status; inventory refreshes every 10 seconds while visible |
| Shells, agents, editors, REPLs, logs | Literal input, optional Enter, Ctrl/Alt A–Z, navigation/editing keys, F1–F12 | Programs interpret the input; no semantic approval hooks are retrofitted |
| Read progress | One-shot bounded visible text or opt-in Follow; visible age and stale/error states | Verified focused-pane Accessibility text; no full PTY stream, ANSI graphics, images, or mouse interaction |
| Create space | New window, new tab in a named window, split an exact pane in four directions | Ghostty must be running; native defaults determine new processes and directories |
| Arrange panes | Zoom/restore, equalize, resize in four directions | Fixed 40-pixel resize steps; selected pane must be focused |
| Navigate output | Top/bottom, page up/down, previous/next prompt | Affects Mac scrollback; prompt navigation needs shell integration |
| Identify work | Rename pane or tab; blank clears the custom title | Focus required; titles limited to 256 UTF-8 bytes without control characters |
| Mac presentation | Increase/decrease/reset font; toggle fullscreen | These change Ghostty on the Mac, not the browser font |
| Close work | Named pane/tab/window confirmation showing known affected panes | Processes may stop; inventory can change concurrently; request acceptance is not proof of disappearance |
| Switch destinations | Separate in-memory draft for each native identity | No persisted drafts; page reload/disconnect discards them |
| Recover interrupted delivery | Durable receipts, status lookup, explicit uncertainty | Never replay a command automatically; user inspection resolves uncertainty |

Clipboard synchronization, arbitrary native actions, global app quit/configuration editing, mouse/graphics forwarding, moving tabs between windows, and a native iPhone client remain outside this pass. Each needs a concrete phone workflow and a reliable scope contract; adding every binding as a button would not make the cockpit safer or easier to use. Background web-service installation and device management also remain separate follow-up work.

## Engineering changes

- The native adapter serializes operations with a context-aware gate. A canceled request waiting behind a slow Ghostty call exits without later running against the app.
- A shared action validator rejects invalid scopes, unrelated fields, unsupported controls, malformed keys, and unsafe title strings before native dispatch. Exact process/window/tab/pane identities remain the destination contract.
- Layout and title controls require focused identity at the HTTP inventory boundary and immediately before the native action. They never automatically focus another pane. Local interaction can still race separate OS calls; this is documented in the threat model.
- Follow waits 2.5 seconds after completion before requesting another snapshot. Selection changes abort old reads. Generation/identity checks discard stale results even if cancellation is delayed. Hidden pages pause; offline or capture errors stop Follow.
- A successful input clears only the matching, unchanged draft. Selecting another pane while awaiting a receipt cannot erase its draft or inherit the previous command.
- Auth, CSRF, origin/host checks, inert text rendering, receipt persistence, and embedded assets remain in place. Added JavaScript dependencies are test-only.

## Full interface review

Mode: **full**. Scope: pairing, selected/empty terminal views, output errors, disconnected/uncertain states, destination picker, composer, control/key sheets, rename/close dialogs, mobile and desktop layouts. Framework: embedded HTML and plain JavaScript; styling: existing plain CSS, no additional styling system. Real Ghostty and physical iPhone behavior remain unverified.

| Category | Evidence inspected | Result |
| --- | --- | --- |
| Typography | Pairing/workspace headings, long titles, terminal monospace, timers, labels at 320/390/1440 widths | Clear hierarchy; tabular changing values; wrapping and overflow checks |
| Surfaces | Output/composer cards, named confirmations, picker/control/key sheets, 44px buttons, landscape scrolling | Structural borders, restrained shadows, safe-area spacing, reachable sheet exits |
| Animations | Hover/press transitions, focus states, reduced-motion emulation | Specific 120ms transitions; no entrance loops or animated output; reduced motion removes transitions and scale |
| Icons | Existing brand icon, native SVG ghost, key/control glyphs, named icon buttons | Decorative art hidden from assistive technology; action labels match visible names, directories, and key names; no icon library added |
| Performance | Embedded assets, Follow scheduling, cancellation, inventory re-rendering, browser errors | No runtime CDN, no overlapping Follow reads, hidden-page pause, no persistent `will-change` |

### Findings addressed

| Severity | Location | Before | After | Why |
| --- | --- | --- | --- | --- |
| HIGH | `internal/web/assets/app.js:247` | One composer could carry a prompt into another shell | Draft map keyed by native identity; matching-receipt clearing | Preserve destination and literal intent |
| HIGH | `internal/web/assets/app.js:616` | Output refresh had no continuous observation mode | Opt-in bounded Follow, cancellation, stale-response rejection, visible age | Honest feedback without overlapping requests or wrong-pane output |
| HIGH | `internal/web/assets/app.js:277`; `internal/ghostty/control.js` | Broader native actions were unavailable and some depend on focus | Named, allowlisted controls gated by exact focused identity | Prevent silent actions on a different pane |
| MEDIUM | `internal/web/assets/index.html:368`; `internal/web/assets/app.js` | Tab-only lifecycle controls | New windows, four splits, scoped pane/tab/window closure, literal titles | Match Ghostty's workspace hierarchy with explicit scope |
| MEDIUM | `internal/web/assets/app.js:378` | Long flat browsing within the existing grouping | Search by title/path, fleet counts, selected/focused distinction | Make multiple agents/shells easy to find |
| MEDIUM | `internal/web/assets/index.html:286`; `internal/web/assets/index.html:451` | Only common quick keys visible | Quick keys plus Ctrl/Alt letter, navigation, and function-key drawer | A phone keyboard needs the terminal's control vocabulary |
| MEDIUM | `internal/web/assets/style.css:973`; `internal/web/assets/style.css:1375` | Expanded drawers introduced narrow keys and long scrolling surfaces | Five-column mobile letter grid, minimum 44px targets, sticky close heading | Keep touch controls usable at 320px and in landscape |
| MEDIUM | `internal/web/assets/index.html:251`; `internal/web/assets/style.css:468` | Minimal visual separation between observing and composing | Dark bounded output, separate composer and destination badge, clear primary Send | Visually distinguish observation, input, and side effects |
| MEDIUM | `internal/web/assets/app.js:378`; `internal/web/assets/app.js` key drawer | Picker and Page Up/Down labels omitted part of their visible names | Include visible names/paths/focus marker and readable key names in accessible labels | Keep voice control and assistive technology aligned with visible buttons |
| LOW | `internal/web/assets/index.html:36`; `internal/web/assets/style.css:743` | Functional pairing card | Small ghost illustration, clearer setup copy, warm amber/mint palette | Give the project personality without cluttering the working screen |
| LOW | `internal/web/assets/style.css` | Basic surface styling | Consistent radius/color tokens, layered shadows, clear structural borders, balanced headings | Improve visual coherence and reading order |
| LOW | `internal/web/assets/style.css:1360`; `internal/web/assets/manifest.webmanifest` | Existing restrained motion and app metadata | Explicit-property press/color feedback, reduced-motion preservation, matching app theme color | Consistent browser and installed-app appearance |

### Considered but rejected

| Location | Candidate | Rejected because |
| --- | --- | --- |
| Output card | Automatic focus while following another pane | Would change the Mac's active work without an explicit action |
| Workspace | Agent activity badges inferred from titles/output | The native API does not establish reliable task or approval semantics |
| Page shell | Fixed full-screen composer positioned from keyboard offsets | Mobile browser keyboard/safe-area behavior varies; natural scrolling is more robust for this version |
| All controls | Expose the complete raw Ghostty action dictionary | Includes global/destructive/clipboard actions with different scope and feedback requirements |
| Pairing/output | Animated ghost, blinking terminal cursor, repeated entrance effects | Adds recurring distraction to a frequently checked control surface |

### Verification and verdict

Automated validation covers the Go adapter and HTTP server with the race detector; six Node VM regressions execute the native script; Chromium and WebKit exercise the full phone workflow against an HTTP fixture. Browser assertions cover uncertain receipt recovery without replay, pane-specific drafts, literal input/titles, focus guards, search, all split directions, empty-inventory creation, close scopes, snapshot cancellation, Follow timing/error pause, blocked browser storage, logout, reduced motion, 320px hit areas and overflow, landscape, and automatically detectable WCAG A/AA issues in pairing, terminal, and sheets. The fixture uses representative native objects; it does not prove Ghostty integration.

Commands: `GOMAXPROCS=2 go test -race -p 2 ./internal/ghostty ./internal/web`; `npm test --prefix web-tests`; `ONIBI_BROWSER=webkit npm test --prefix web-tests`. `ONIBI_CHROME_PATH` was used for the locally installed Chrome. CI runs both browser engines, Go vet/race/build on Linux/macOS, vulnerability scanning, and a native release snapshot.

**Verdict: Approve the interface implementation subject to the explicit validation boundaries below.** No known actionable interface-polish finding remains after the automated and screenshot review. This is not a native production-readiness sign-off.

**Not verified:** physical iPhone/Safari keyboard and VoiceOver; real Tailscale pairing; successful live native input/output and expanded controls. The earlier Ghostty 1.3.1 smoke stopped answering during focus. Per the maintainer's instruction, this pass uses hermetic checks and documents that gap; it does not restart Ghostty or manipulate existing work. A 10%-speed browser animation-panel review was not performed; the interface uses short CSS press/color transitions, inspected through screenshots, computed styles, and reduced-motion tests.

The next useful validation is a disposable native window on a responsive Ghostty installation: pair the phone, send/read a harmless marker, split/select/rename panes, exercise Ctrl C in a harmless process, test Follow while changing focus locally, close the disposable scope, and reconnect. Record latency and physical-keyboard friction before broadening the feature set.
