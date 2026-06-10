# PRD: Ghostty + tmux ⇄ Telegram Bridge

A local-only background service that notifies you on Telegram when work inside your Ghostty terminal finishes or needs a decision, and lets you reply from Telegram — sending free text or preset commands straight into the right tmux pane (your shell *or* an AI agent like Claude Code, depending on what owns that pane).

---

## 0. Critical context for the implementer (read first)

- **Ghostty is a terminal emulator, not an agent.** It renders text and knows when a *shell command* finishes (via OSC 133 shell integration / `notify-on-command-finish`, added in Ghostty 1.3, March 2026). It does NOT know when an AI agent "finishes thinking." Those two trigger sources are different and both are in scope.
- **Ghostty has no stable external control/scripting socket** for injecting keystrokes into a pane (it has been discussed upstream but is not shipped). **Therefore we do not rely on Ghostty for injection at all.**
- **tmux is the control plane.** All sessions run inside tmux, which Ghostty merely hosts/renders. Injection is done with `tmux send-keys -t <target-pane>`, which is reliable, scriptable, and identical on macOS and Linux. This also solves context routing: each thing (a shell, a Claude Code session) lives in a known tmux pane, and we target the right pane by name.
- **Two trigger sources, one Telegram bridge:**
  1. *Shell command finished* → zsh `precmd`/`preexec` hook (OSC 133-style) fires a notify script.
  2. *AI agent done / awaiting input* → the agent's own hook (e.g. Claude Code `Stop` hook) fires the same notify script.
- **Platform:** macOS + zsh first. Architecture must be cross-platform (Linux next) and multi-shell (bash, fish later). Keep OS- and shell-specific bits in isolated modules.
- **Scope for v1:** the full thing — notify out, free-text replies in, and preset commands.

## 1. Goals

- Telegram notification when (a) a tracked shell command finishes, or (b) a tracked AI-agent session finishes a turn / is waiting for input.
- From Telegram, send **free text** that is injected into a chosen tmux pane (lands in the shell prompt or the agent's prompt depending on what's running there).
- From Telegram, trigger a small **allowlist of preset commands** (buttons / slash-commands).
- **Local only:** outbound HTTPS to Telegram exclusively. No public URL, no webhook, no inbound ports. Runs only while enabled.
- **Secure:** single authorized Telegram user; no arbitrary shell from chat; secrets in env.

## 2. Non-Goals

- No cloud hosting, webhooks, or always-on server.
- No dependency on a Ghostty control socket.
- No multi-user support, no web UI.
- Not responsible for installing Ghostty, tmux, or any agent.
- No arbitrary remote shell: system actions limited to the preset allowlist; free text goes only to `send-keys` into a pane.

## 3. Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│  macOS / Linux laptop  (runs only while bridge is enabled)           │
│                                                                      │
│   Ghostty (renders)                                                  │
│   └── tmux server ──────────────── CONTROL PLANE ───────────────┐    │
│        ├── pane "shell"   (zsh)                                  │    │
│        │     └─ precmd/preexec hook ──► notify(cmd_done,…) ──┐   │    │
│        ├── pane "claude"  (Claude Code TUI)                  │   │    │
│        │     └─ Stop / Notification hook ─► notify(agent_*,…)│   │    │
│        └── pane "build"   (long task)                        │   │    │
│                  ▲                                           │   │    │
│      tmux send-keys -t <pane>  (INJECTION)                   │   │    │
│                  │                                           ▼   │    │
│            ┌─────┴───────────────────────────────────────────────┐  │
│            │                 Bridge Core (Python)                 │  │
│            │  • event intake (local Unix socket / FIFO)           │  │
│            │  • state per tracked pane (idle/running/awaiting)    │  │
│            │  • router: Telegram msg → send-keys OR preset action │  │
│            │  • debounce, auth filter                             │  │
│            └─────┬───────────────────────────────────────┬────────┘  │
│                  │ tmux send-keys / list-panes            │ notify     │
│                  ▼                                        │            │
│            tmux CLI                          ┌────────────┴─────────┐  │
│                                              │  Telegram Client      │ │
│                                              │  getUpdates long-poll │ │
│                                              │  sendMessage + buttons│ │
│                                              └────────────┬─────────┘  │
└───────────────────────────────────────────────────────────┼──────────┘
                                                             │ HTTPS (outbound only)
                                                             ▼
                                                     Telegram Bot API → your phone
```

**Event flow in:** hooks (shell or agent) call a tiny `ghostty-notify` client that writes a JSON event to the bridge's local Unix socket. The bridge formats it and sends a Telegram message.

**Command flow out:** a Telegram reply or button hits the long-poll loop → the bridge resolves the target pane → `tmux send-keys -t <pane> -l "<text>"` then `Enter` (configurable) → the keystrokes land wherever that pane's foreground process is (shell or agent).

## 4. Components

### 4.1 Telegram Client
- **Outbound:** `sendMessage` (inline keyboards for presets), `answerCallbackQuery`, optional message edits for status.
- **Inbound:** `getUpdates` long-polling (~30s timeout), persisting `offset` so messages aren't reprocessed across restarts. **No webhooks** — keeps it local and laptop-friendly.
- Library: `python-telegram-bot` (v21+, async) recommended; raw `httpx` acceptable if minimizing deps. Implementer's call.

### 4.2 Event Intake (local IPC)
- A **Unix domain socket** (path from `$GHOSTTY_TG_BRIDGE_SOCKET`, default `~/.local/state/ghostty-tg-bridge/bridge.sock`) the bridge listens on.
- A small CLI client `ghostty-notify` (can be a short shell or Python script) that hooks invoke, e.g.:
  `ghostty-notify --type cmd_done --pane "$TMUX_PANE" --status "$?" --cmd "$LAST_CMD" --tail "<recent output>"`
- Event JSON schema:
  ```json
  {
    "type": "cmd_done | agent_done | agent_awaiting | agent_message | session_exited",
    "pane": "%3",                 // tmux pane id ($TMUX_PANE)
    "pane_label": "claude",       // resolved human label if available
    "status": 0,                   // exit code for cmd_done
    "cmd": "npm run build",        // for cmd_done
    "text": "…",                  // for agent_message/awaiting
    "tail": "last ~500 chars",    // optional output tail
    "ts": 1730000000
  }
  ```
- Must tolerate the bridge being **off**: if the socket isn't there, `ghostty-notify` exits 0 silently (never block the user's shell/agent).

### 4.3 Bridge Core
- Listens on the socket; maintains per-pane state (`idle | running | awaiting`) for debounce and sensible notification text.
- **Auth filter:** every inbound Telegram update checked against `ALLOWED_CHAT_ID`; others dropped + logged.
- **Router:**
  - Free text → resolve target pane (see 4.5) → `tmux send-keys`.
  - Preset (button callback or `/cmd`) → preset allowlist runner.
  - `/status`, `/panes`, `/target <label>` control commands (see 4.6).
- **Debounce:** collapse rapid duplicate `cmd_done` events; suppress notifications for commands faster than a configurable threshold (mirrors Ghostty's `notify-on-command-finish-after`, e.g. 5s) unless from an agent hook.

### 4.4 tmux Controller (injection + introspection)
- Wraps tmux CLI:
  - Inject: `tmux send-keys -t <pane> -l -- "<text>"` then optionally `tmux send-keys -t <pane> Enter`. Use `-l` (literal) so text isn't interpreted as key names; send `Enter` as a separate, explicit call.
  - Discover: `tmux list-panes -a -F '#{pane_id} #{pane_title} #{pane_current_command} #{session_name} #{window_name}'` to map labels ↔ pane ids and show what's running.
  - Capture tail: `tmux capture-pane -p -t <pane> -S -50` for an output snippet in notifications when a hook didn't supply one.
- **Pane labeling:** support a stable label per pane. Options (implementer pick, document choice): user names panes/windows (`tmux rename-window claude`), or the bridge maps `pane_current_command` (e.g. a pane running `claude` → label "claude"). Prefer current-command detection with user-overridable aliases in config.

### 4.5 Target resolution (which pane gets the reply)
This is the heart of the "context-aware" behavior. When a Telegram free-text reply arrives, decide the target pane in this priority order:
1. **Reply-to context:** if the user is replying to a specific notification message, route to the pane that notification came from. (Store a map: Telegram `message_id` → `pane_id`.) ← preferred, most intuitive.
2. **Explicit target:** an active `/target <label>` selection set by the user.
3. **Most-recent-event pane:** the pane that last fired an event.
4. If ambiguous, ask via an inline keyboard listing current panes.

Because injection is just keystrokes into a pane, the text **automatically lands in whatever owns that pane's foreground** — Claude Code's prompt if Claude Code is running there, the zsh prompt otherwise. The bridge does not need to understand the agent; it only needs the right pane. (Document this clearly — it's the user's mental model from the requirements.)

### 4.6 Preset Command Runner
- Config-defined **allowlist**: `key → label → action`. Action is one of:
  - `inject:<text>` into the resolved/target pane (e.g. "Approve" → `inject:yes`, "Continue" → newline, "Interrupt" → send `C-c`).
  - `internal:<name>` for bridge actions: `internal:show_tail`, `internal:list_panes`, `internal:stop_session` (kill a tracked process/pane per config), `internal:select_target`.
- Presets render as inline-keyboard buttons on `agent_awaiting` notifications and via `/menu`.
- **Hard rule:** preset actions are matched by key only and resolve to *predefined* commands. No Telegram-supplied string is ever assembled into a shell command. Free text only ever becomes `send-keys` literal input.

### 4.7 Hook installers (per trigger source)
Provide setup scripts/snippets, isolated per shell/agent:
- **zsh (v1):** append `precmd`/`preexec` functions to `~/.zshrc` (guarded block) that call `ghostty-notify`. `preexec` records the command + start time; `precmd` fires `cmd_done` with exit status and elapsed, respecting the min-duration threshold. (This is the portable equivalent of OSC 133 markers; if Ghostty's own shell integration is enabled, we can additionally lean on it, but our hook is self-sufficient.)
- **Claude Code:** add a `Stop` hook (and `Notification`/awaiting hook if available) in Claude settings that runs `ghostty-notify --type agent_done --pane "$TMUX_PANE" …`. (Pattern matches the known Ghostty+Claude notification gist, but points at our socket instead of `osascript`.)
- **Endstate (later phases):** bash (`PROMPT_COMMAND` / `bash-preexec`), fish (`fish_postexec`), and a generic OSC 133 listener. Keep each in `hooks/<shell>/`.

## 5. Functional Requirements

- **FR1 Command-done notify:** zsh `precmd` fires `cmd_done`; bridge sends a Telegram message (command, exit status ok/fail, elapsed, optional output tail). Respects min-duration debounce.
- **FR2 Agent-done / awaiting notify:** Claude Code `Stop`/awaiting hook fires `agent_done`/`agent_awaiting`; bridge notifies and, for `agent_awaiting`, attaches preset buttons + a hint to reply with free text.
- **FR3 Free-text injection:** an authorized free-text reply is injected via `tmux send-keys` into the resolved target pane, then acknowledged. Replying to a specific notification routes to that notification's pane (FR target-resolution).
- **FR4 Presets:** preset buttons / `/cmd` execute allowlisted actions (inject or internal) and report results.
- **FR5 Pane control:** `/panes` lists tracked panes + what's running; `/target <label>` sets the default injection target; `/status` shows per-pane state and bridge uptime.
- **FR6 Enable/disable:** single entrypoint to start (`ghostty-tg-bridge run`) and clean stop (SIGINT/SIGTERM) that closes the socket and stops polling. When stopped, `ghostty-notify` calls no-op silently.
- **FR7 Exit handling:** `session_exited` notifies and updates pane state.
- **FR8 Setup helpers:** `ghostty-tg-bridge get-chat-id` (prints your chat id via `getUpdates`) and `ghostty-tg-bridge install-hooks [--shell zsh] [--agent claude]` (idempotent hook installation with guarded blocks).

## 6. Non-Functional Requirements

- **NFR1 Local-only:** outbound HTTPS to `api.telegram.org` only; no inbound listeners except the local Unix socket (file-permissioned to the user, `0600`).
- **NFR2 Resilience:** `getUpdates` retried with backoff; survive sleep/wake; never crash the user's shell or agent if the bridge is down (intake client fails open/silent).
- **NFR3 Low idle cost:** long-poll ~30s; negligible CPU idle.
- **NFR4 Cross-platform & multi-shell:** Python 3.11+. OS/shell specifics isolated in modules (`hooks/`, `platform/`). v1 verified on macOS+zsh; structure ready for Linux/bash/fish.
- **NFR5 Observability:** leveled local log file; **redact bot token**; log dropped unauthorized updates.

## 7. Security

- **SEC1 Single user:** enforce `ALLOWED_CHAT_ID` on every update; ignore + log others.
- **SEC2 Secrets:** `TELEGRAM_BOT_TOKEN`, `ALLOWED_CHAT_ID` from env/`.env` (git-ignored). Never commit, never log the token.
- **SEC3 No shell from chat:** free text → only `tmux send-keys -l` (literal keystrokes into a pane). System actions → only the preset allowlist.
- **SEC4 Closed allowlist:** presets editable only in config, not via chat.
- **SEC5 Socket perms:** Unix socket `0600`, under `$XDG_STATE_HOME`/`~/.local/state`.
- **SEC6 Injection caution:** since literal text lands in a live pane, document the risk and optionally gate a few destructive presets (e.g. `stop_session`) behind a confirm tap.

## 8. Configuration

`config.yaml` + `.env`:
- env: `TELEGRAM_BOT_TOKEN`, `ALLOWED_CHAT_ID`
- `socket_path` (default `~/.local/state/ghostty-tg-bridge/bridge.sock`)
- `panes.aliases`: map of label → match rule (by `pane_current_command`, title, or explicit pane id)
- `panes.default_target`: optional label
- `notifications.min_command_seconds` (default 5), `include_output_tail` (bool), `output_tail_lines` (default 50)
- `injection.send_enter` (bool, default true), `injection.literal` (default true)
- `presets`: list of `{ key, label, action }` (`inject:<text>` | `internal:<name>`)
- `confirm_destructive`: list of preset keys requiring a confirm tap
- `logging.level`, `logging.file`
- Ship `config.example.yaml` + `.env.example`.

## 9. Setup (README)

1. Install prerequisites: Ghostty, tmux, Python 3.11+. (User-provided; just check and warn.)
2. Create a Telegram bot via **@BotFather** → `/newbot` → copy token into `.env`.
3. `ghostty-tg-bridge get-chat-id` → message the bot once → it prints your chat id → put in `.env`.
4. Edit `config.yaml` (pane aliases, presets).
5. `ghostty-tg-bridge install-hooks --shell zsh --agent claude` (idempotent; writes guarded blocks to `~/.zshrc` and Claude settings).
6. Run all your terminal work inside tmux (the README should give a one-liner: have Ghostty launch `tmux new -A -s main`, or document `command = tmux ...` in Ghostty's config).
7. Start the bridge: `ghostty-tg-bridge run`. Stop with Ctrl-C.

## 10. Project Structure

```
ghostty-tg-bridge/
├── README.md
├── config.example.yaml
├── .env.example
├── pyproject.toml                 # python-telegram-bot (or httpx), pyyaml
├── ghostty_tg_bridge/
│   ├── __init__.py
│   ├── cli.py                     # run | get-chat-id | install-hooks
│   ├── config.py                  # load + validate config/secrets
│   ├── core.py                    # event router, per-pane state, debounce, auth
│   ├── telegram_client.py         # long-poll, sendMessage, inline keyboards
│   ├── intake.py                  # Unix-socket event server + JSON schema
│   ├── tmux.py                    # send-keys, list-panes, capture-pane, target resolution
│   ├── presets.py                 # allowlist runner
│   └── platform/                  # OS-specific helpers (paths, perms)
├── clients/
│   └── ghostty-notify             # tiny intake client invoked by hooks (fails open)
├── hooks/
│   ├── zsh/precmd-preexec.zsh
│   ├── claude/settings-snippet.json
│   ├── bash/                      # later
│   └── fish/                      # later
└── tests/
    ├── test_core.py               # routing, auth drop, debounce, target resolution
    ├── test_tmux.py               # send-keys/list-panes (mock subprocess)
    ├── test_intake.py             # socket schema, fail-open when bridge down
    └── test_presets.py            # allowlist enforcement, no-shell guarantee
```

## 11. Implementation Phases

1. **Scaffold + Telegram out.** Config/secrets, `get-chat-id`, long-poll loop, single-user auth, `sendMessage`. Fake event to confirm notifications arrive.
2. **Intake socket + zsh hook.** Unix-socket server, `ghostty-notify` client (fail-open), `precmd`/`preexec` install → real `cmd_done` notifications with status/elapsed/tail. (FR1, FR8)
3. **tmux injection + target resolution.** `send-keys`, `list-panes`, reply-to-message → pane mapping, `/panes` `/target` `/status`. Free-text replies land in the right pane. (FR3, FR5)
4. **Presets.** Inline keyboards + `/cmd`, allowlist runner, confirm-destructive. (FR4)
5. **Claude Code agent hook.** `Stop`/awaiting hook → `agent_done`/`agent_awaiting` with preset buttons. Verify injection lands in Claude's prompt. (FR2)
6. **Hardening + portability.** Backoff/retry, sleep-wake, logging/redaction, debounce tuning. Stub bash/fish hook installers and a generic OSC 133 listener for the cross-platform endstate.

## 12. Acceptance Criteria

- A >5s shell command finishing in a tracked pane produces exactly one Telegram notification with command + status (no duplicates; sub-5s commands suppressed by default).
- Claude Code finishing a turn in its pane produces a notification with preset buttons; tapping "Continue" advances it.
- Replying to that specific notification with free text injects into Claude's prompt (not the shell), via `tmux send-keys`.
- Replying to a shell-pane notification injects a command into the shell.
- A message from any non-authorized chat id is ignored and logged.
- No Telegram-supplied string is ever passed to a shell; free text only becomes literal `send-keys` input.
- Stopping the bridge ends polling and closes the socket cleanly; with the bridge off, shells and Claude Code run normally and `ghostty-notify` is a silent no-op.
- Bot token never appears in logs or committed files; socket is `0600`.

## 13. Risks & Notes

- **OSC 133 vs custom hook:** Ghostty 1.3 can emit its own command-finish notifications, but those are local desktop notifications and can't reach Telegram or carry our metadata. We use our own zsh hook for the data and let Ghostty's native notifications coexist (user can disable one). Don't depend on Ghostty internals.
- **No Ghostty control socket:** confirmed not available; all injection is via tmux. If Ghostty ships a scripting API later, it could become an optional alternative backend, but tmux remains the portable default.
- **Pane identity across restarts:** pane ids (`%3`) change between tmux sessions; prefer stable labels (window/pane names or current-command aliases) and re-resolve on each event.
- **SSH/remote panes:** `$TMUX_PANE` and hooks won't propagate into remote shells over SSH automatically; out of scope for v1 (note it).
- **fish/bash/Nushell:** hook mechanisms differ; isolated per-shell installers planned for later phases.

## 14. Also reference

- https://github.com/antirez/tgterm basically this github repo
