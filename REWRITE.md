# Onibi Rewrite Findings

## Product thesis

Onibi should be a polished, Telegram-native remote command center for a solo developer's persistent local terminal sessions and coding agents.

The core job is to let one owner use a phone to continue work in named tmux sessions: run arbitrary commands, send input, inspect output, monitor agent progress, make decisions, and recover a session. It is not a team product, hosted control plane, or browser-terminal product.

## v1 boundary

Keep:

- Telegram owner pairing and bot control.
- Persistent, named tmux sessions with multi-session selection.
- Plain-message literal terminal input, defaulting to Enter.
- Explicit paste mode for literal multi-line input without an implicit Enter.
- Session status, screenshots, bounded output, interrupt, Escape, Enter, and kill controls.
- Durable audit, recovery, reconnect, and idempotent decisions.
- Codex as the first semantic agent integration.
- Pi tool-approval cards after event-payload validation.
- Claude Code permission and completion hooks.

Cut or hide:

- PWA, QR pairing, local CA, browser WebSockets, and Web Push.
- Ghostty handover.
- LAN, Tailscale, WireGuard, ZeroTier, Cloudflare, and ngrok web transports.
- Generic agent-hook catalogue and third-party adapter manifests.
- Public relay/E2E complexity.
- Workspace metadata and snapshot/fork features.
- Automatic semantic inference from arbitrary terminal output.

## Interaction model

Use two lanes.

1. Generic tmux lane: every terminal program gets literal input, bounded text output, screenshots, and static terminal controls. Do not infer choices from terminal text or screenshots.
2. Semantic agent lane: adapters turn documented structured agent events into Telegram-native decisions.

| Scenario | Telegram UX | Execution |
| --- | --- | --- |
| Normal shell command | Plain message sends literal input plus Enter | Selected tmux session |
| Multiline paste | `Paste next message` state, then explicit Enter | Literal bytes |
| Short result | One edited status/result card | Coalesced terminal output |
| Long logs | Working card; `/tail`, `/screen`, or file on demand | Bounded capture |
| Agent progress | Edited status card, then final result card | Structured agent events where available |
| Agent approval/choice | Screenshot plus inline decision buttons | Structured callback |
| Free-form agent question | Card plus reply capture or `Other…` | Resume agent with supplied text |
| Generic TUI prompt | Screenshot plus `Enter`, `Esc`, and `Ctrl-C` | No inferred decision |
| Multi-session work | Session picker and session-bound cards | Explicit session ID |
| Exit/failure | Final status card with duration and screen | Persistent audit event |

## Telegram presentation rules

Telegram should be an operations console, not a log spam bot.

- Use an inline callback keyboard for approvals and choices, not a Telegram poll. A decision is an immediate, single-owner action; a poll is a vote.
- Persist decision state locally. Callback payloads must be opaque, short action tokens.
- Atomically apply a selection, resume or deny the underlying action, then edit the decision card to its final state.
- Require a second confirmation for high-risk approvals.
- Coalesce streaming output into one editable status message, then send a final state card. Do not send one message per output chunk.
- Default to a screenshot when a generic session updates, a Pi turn ends, a structured session becomes actionable/completes/fails, or the owner invokes `/screen`.
- For generic interactive programs, a screenshot is visual context only. The bot must not pretend it knows what a screen means.

## Screenshot design

Render the managed tmux/terminal state, not the Ghostty window. Capture tmux's resolved screen rather than replaying escape sequences; this keeps screenshots headless, Unicode-safe, and independent of macOS screen-recording permissions.

The repository uses `internal/render/png.go` and `internal/telegram/api.go` to send rendered managed-session screens on state transitions or on demand, never continuously.

## Agent design

### Codex

Use Codex App Server as the semantic integration rather than screen-scraping the Codex TUI. It exposes streamed events and structured requests for command execution, file change, permission, and user-input decisions. Map those requests to Telegram inline cards such as:

```text
[terminal screenshot]

Codex needs network access
Session: onibi-api
Reason: install dependencies
Scope: api.github.com:443

[Allow once] [Allow for session]
[Deny]       [Cancel]
```

Do not expose Codex App Server's `thread/shellCommand` as a Telegram convenience command: it runs outside the thread sandbox with full access. Arbitrary terminal commands stay in the managed tmux shell.

### Pi

The existing Pi extension reports session, input, and tool events and can block or edit tool input. Reuse the approval-card model, but do not claim native mapping for every Pi prompt until live event payloads are validated.

### Claude Code

Run Claude Code in the managed tmux lane with an Onibi-owned settings file. Map documented `PermissionRequest` hooks to the approval-card model and `Stop`/`StopFailure` hooks to an edited final card plus fresh screen. Do not infer generic terminal prompts or override the user's Claude settings outside that session.

## Implementation invariants

- Generic terminal input is literal. No output parser promotes a TUI prompt into a decision.
- Generic input is never mislabeled as completion; Pi/Claude lifecycle completion and structured Codex events send fresh final state.
- Screens use a bundled, owner-selectable monospaced font; external fonts are opt-in local paths.
- Callback state is session-bound, opaque, persisted, 24-hour bounded, and single-use.
- Codex streaming output is coalesced into an edited status card rather than emitted per chunk.
- Codex approval and input requests are only answered while the corresponding App Server request remains pending.
- Pi waits are fail-closed. A daemon restart cancels an in-flight Pi approval because the original blocking process cannot safely be resumed.
- Claude permission waits are fail-closed. Its per-session settings file is private to Onibi's state directory.
- Audit records hash terminal payloads instead of storing them in the audit table.

## Sources

- Telegram Bot API: <https://core.telegram.org/bots/api>
- Telegram bot rate limits: <https://core.telegram.org/bots/faq>
- tmux advanced use and control mode: <https://github.com/tmux/tmux/wiki/Advanced-Use>, <https://github.com/tmux/tmux/wiki/Control-Mode>
- Codex App Server: <https://learn.chatgpt.com/docs/app-server>
- Codex approvals and sandboxing: <https://learn.chatgpt.com/docs/agent-approvals-security>
- Claude Code hooks: <https://code.claude.com/docs/en/hooks>
