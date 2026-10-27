# Changelog

## 0.1.0 (2026-10-27)

First tagged release.

### Upgrading from a source install

- Telegram, the agent hooks, the approval queue and the Codex App Server transport are gone. Config files that still set their retired keys load without error; the keys are ignored.
- Opening an existing state database drops the retired Telegram, `approvals` and `claude_questions` tables, so a downgrade cannot recover them.
- A bot token stored by an earlier release is not deleted; remove it from the keychain or `.env` file yourself.
- Delete the Pi extension from an older release (by default `~/.pi/agent/extensions/onibi.ts`); `onibi doctor` fails while it remains.

### Changes

- Added renaming of persistent sessions: `onibi session rename <name|id> <new-name>` on the Mac, and **Rename** next to Interrupt on the phone. Names use letters, digits, dot, underscore and hyphen and stay unique among live sessions; the multiplexer's own session name is unchanged.
- New session arguments on the phone now follow shell quoting: single quotes, double quotes and backslash escapes group words, with no variable, glob or command expansion. An unclosed quote is reported instead of being sent.
- Redesigned the phone's look around an animated ASCII fire and block-letter "Onibi" art on the pairing page and header. The tab is titled ONIBI (with the waiting count first), and explanatory filler copy is gone. Text size, wrapping and snapshot start/end moved into a Settings dialog at the top right.
- Removed the Ghostty Controls drawer (layout, scrollback, font, fullscreen and pane/tab title controls) from the phone for now. New tab, Split pane and Close tab remain.
- Fixed push subscriptions from Chrome being rejected for their `expirationTime` field, and a race where toggling notifications could show Off after subscribing.

- Added opt-in push notifications, turned on per browser in Devices. A browser is notified when a persistent session starts needing input or rings the bell; the notification names the session and reason, never its output, and opens the session when tapped. Delivery uses Web Push with VAPID and RFC 8291 encryption implemented on the standard library, and only reaches browser vendors' push services. Revoking a browser removes its subscription. Onibi can now be added to an iPhone Home Screen with a proper icon, which iOS requires for notifications.
- Added New session to the phone. It starts a shell or any program in a folder of an open pane or session, or in a typed absolute path, using a durable receipt, then selects the new session. The web login service now keeps the `PATH` from install time, like the daemon service, so programs in folders such as `~/.local/bin` are found.
- Added quick replies. When a Ghostty pane or persistent session is waiting on a yes/no question, a key press or a numbered menu, the phone offers one-tap replies that send the same key a person would press. Prompt detection moved into a shared package, so the daemon and the web server recognise the same prompts.
- Tightened the phone interface. Forget this browser moved from the header into Devices. Session and device expiry read as relative times, such as "expires in 12 hours". The footer and filler helper lines are gone, and Ghostty status no longer wraps in the header.
- `onibi web` now runs the sessions daemon itself when none is running, so one command, or one `onibi web service`, gives the phone both Ghostty and persistent sessions. The daemon stops with the web server and its sessions are restored at the next start. `onibi start` still runs the daemon alone, and now refuses to start when a daemon is already running.
- Added `onibi session attach` (execs the multiplexer's own attach, or switches client inside tmux) and `onibi session kill` (asks for confirmation on a terminal, otherwise requires `--yes`). `session list` (alias `ls`) prints a table with live attention, `--all` and `--json`. `session new` starts in the current folder and accepts `--attach`. Session names complete in the shell.
- Added `onibi --version`, help text for every command, and a labelled `onibi system status` with `--json`.
- Added persistent sessions to the web cockpit. When `onibi start` is running, its tmux, Zellij and Screen sessions appear above the Ghostty windows. Their scrollback and screen can be read without focus, tmux output keeps its colours, and input, keys, interrupt and a confirmed end-session use the durable receipts. Sessions keep working while Ghostty is unavailable. The intake socket gains exact-ID session list, capture and key RPCs.
- Added generic attention signals for managed sessions: needs input (prompt patterns on the cursor line), bell (a tmux bell counter on started sessions, and the bell flag on adopted ones), active, idle and quiet. The phone shows them as badges and counts waiting sessions in the page title. See [managed sessions](docs/managed-sessions.md).
- Added recall of recent sends and copies on the phone: up to 20 delivered inputs and copied output snippets, inserted into the draft with a tap and never sent automatically. The list stays in page memory only.
- `onibi session new` runs the configured shell or any program on `PATH` (or by absolute path), passing its arguments unchanged. Claude is no longer launched with Onibi hook settings or refused flags: unanswered hooks stalled its permission requests for five minutes before denying them, and denied every `AskUserQuestion` after three. `doctor` no longer checks for specific agent binaries.
- Added digit keys and plain letters to the phone's key drawer. They are sent as single key presses on every backend, so agent and installer menus that choose with `1`, `y` or `n` respond. Ghostty panes receive them through its `text:` action, because its `send key` produces no character for an unmodified key.
- Zellij sessions now receive text as typed characters instead of a bracketed paste, matching tmux and Screen.
- Removed `onibi pi install` and `onibi pi status`. Pi runs plainly like every other program. `onibi doctor` now fails while the extension from an older release remains (by default `~/.pi/agent/extensions/onibi.ts`), because it blocks Pi tool calls in Onibi sessions; delete the file and run `/reload` in Pi.
- `onibi session new` and `onibi session adopt` now print the daemon's reason and exit non-zero when it refuses a request, instead of printing an empty result.
- `onibi session new codex` now runs the interactive Codex CLI in a multiplexer, so it gets the same reading, input and attention signals as any other program. The Codex App Server transport is no longer started, and App Server sessions left from earlier versions are marked ended on restart.
- Removed the remaining agent integration code: the tool approval queue, Claude question store, Codex App Server client, Pi extension template and the `onibi-notify` hook client. Builds, release archives and source installs now contain only `onibi`; `make update` and `make uninstall` remove an `onibi-notify` an older install recorded while it still matches its checksum. Opening an existing state database drops the `approvals` and `claude_questions` tables and stored Codex thread IDs. The `daemon.approval_timeout`, `daemon.claude_question_timeout`, `daemon.approval_sweep_interval` and `daemon.max_subscribers` settings are gone; config files that set them still load. The intake socket no longer accepts `approval_request`, `claude_question` or `agent_lifecycle` requests, and `onibi system status` no longer reports `codex_sessions`.
- Removed code left from Telegram: PNG screenshot rendering with its bundled Nerd Fonts (about 8 MB of the binary), upload staging, the keychain secret store, the per-session output buffer, session resizing and the log redaction handler, along with the `golang.org/x/image` and `99designs/keyring` dependencies. The `screen.font`, `screen.font_path`, `daemon.upload_ttl`, `daemon.upload_max_bytes` and `daemon.output_buffer_bytes` settings are gone; config files that set them still load. The daemon deletes the retired `uploads` folder in the state directory on start. A bot token stored by an earlier release is left in the keychain or `.env` file; see [managed sessions](docs/managed-sessions.md#removed-integrations).

- Removed the Telegram transport: the `onibi telegram` commands, bot-token doctor checks, `telegram-*` make targets, live Telegram end-to-end test, and the `gotd/td` dependency. `onibi start` now runs the managed-session daemon without credentials. Opening an existing state database drops the retired Telegram tables and keys. Managed sessions, agent hooks, and screenshot rendering remain in the tree without a decision surface; see [managed sessions](docs/managed-sessions.md).

- Retired the repository's public GitHub Pages documentation site. The Ghostty cockpit continues to run on the Mac over private Tailscale HTTPS.

- Added passive `onibi web doctor` diagnostics, explicit native probes, typed availability errors, and Ghostty version capabilities.
- Added a Mac readiness sheet and distinct phone states for connection, native inventory, focused capture, permissions, and session expiry. Recovery targets the failed capability and preserves pane drafts.
- Coalesced concurrent inventory polls, added bounded failure backoff, retained fresh mutation preconditions, and reduced redundant post-input observation. Rejected actions immediately reconcile readiness; lost connections stop Follow.
- Added Chromium/WebKit integration tests against the real Go HTTPS handler, embedded UI, and SQLite receipts, including crash recovery, restart revocation, and session expiry. Release archives now use the current documentation files.

- Expanded the Ghostty cockpit with native window creation, four-direction splits, named pane/tab/window closure, pane/tab titles, and allowlisted layout, scrollback, font, and fullscreen controls.
- Redesigned the phone interface with a searchable window/tab/pane navigator, amber/mint surfaces, control and key drawers, Ctrl/Alt letters and function keys, per-pane memory-only drafts, output freshness, and opt-in bounded Follow.
- Made native request queuing cancellable and prevented canceled output reads from replacing another pane's view. Added native-script fixtures, Chromium/WebKit browser regressions, and automated accessibility checks.

- Added a Mac-served, phone-friendly Ghostty interface with private Tailscale HTTPS, browser pairing, exact native targets, bounded focused-pane text, input/keys, and tab creation and confirmed closure.
- Added durable web action receipts, interrupted-delivery recovery, session revocation, and explicit output capability states.
- Retained optional Telegram control of named tmux, Zellij and Screen sessions, rendered screenshots, Codex App Server decisions, and Pi/Claude hooks.
- Fixed Telegram pairing and literal paste safeguards, target-bound kill confirmation, outbox replacement races, hook cancellation, Codex shutdown, configured terminal lifecycle handling, secret storage, upload integrity and CLI diagnostics.
- Updated patched Go dependencies and CI coverage for native releases and the mobile interface. See [the audit](docs/product-audit.md) for findings and remaining product gaps.
