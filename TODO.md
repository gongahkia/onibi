# ONIBI TODO

Last researched: 2026-06-18

Scope: P0/P1 QOL work after the Codex hook schema incident. This is a product and engineering action list, not release notes.

## Operating Rule

- Third-party agent config files must contain only provider-supported schema fields unless that provider explicitly documents an open extension surface.
- Onibi metadata should live in Onibi state DB, a generated plugin source constant, a wrapper command environment variable, or an Onibi-owned sidecar file.
- Do not auto-trust Codex hooks. Codex intentionally requires user review for non-managed command hooks.
- Hook installation must be reversible and inspectable before users open the agent.
- Phone UX must default to buttons and state cards, not raw command memorization.

## Source References

- Codex hooks: https://developers.openai.com/codex/hooks
- Codex manual fetched by skill: `/var/folders/55/fw1sdntj20v8650vsmkn7s8c0000gn/T/openai-docs-cache/codex-manual.md`
- Claude Code hooks: https://code.claude.com/docs/en/hooks
- Claude Code hook guide: https://code.claude.com/docs/en/hooks-guide
- Gemini CLI hooks reference: https://github.com/google-gemini/gemini-cli/blob/main/docs/hooks/reference.md
- Goose hooks: https://goose-docs.ai/blog/2026/05/14/goose-hooks/
- OpenCode plugins: https://open-code.ai/en/docs/plugins
- Amp plugin manual: https://ampcode.com/manual
- Amp plugin API: https://ampcode.com/manual/plugin-api
- Pi extensions: https://pi.dev/docs/latest/extensions
- GitHub Copilot hooks reference: https://docs.github.com/en/copilot/reference/hooks-reference
- GitHub Copilot CLI hook guide: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/use-hooks
- Telegram Bot features: https://core.telegram.org/bots/features
- Telegram Bot API: https://core.telegram.org/bots/api

## Current Code References

- Hook CLI: `internal/cli/hooks.go`
- Adapter registry: `internal/adapters/registry.go`
- Codex adapter: `internal/adapters/codex/install.go`
- Claude adapter: `internal/adapters/claude/install.go`
- Gemini adapter: `internal/adapters/gemini/install.go`
- Goose adapter: `internal/adapters/goose/install.go`
- OpenCode adapter: `internal/adapters/opencode/install.go`
- Amp adapter: `internal/adapters/amp/install.go`
- Pi adapter: `internal/adapters/pi/install.go`
- Copilot adapter: `internal/adapters/copilot/install.go`
- Shell adapter: `internal/adapters/shell/install.go`
- Doctor: `internal/doctor/doctor.go`
- Telegram commands: `internal/telegram/commands.go`
- Telegram keyboards: `internal/telegram/keyboards.go`
- Daemon command handling: `internal/daemon/commands.go`
- Approval rendering/callbacks: `internal/daemon/approvals.go`
- Approval edit validation: `internal/approval/schema.go`
- Encrypted Mini App: `docs/miniapp/index.html`, `internal/daemon/encrypted.go`, `internal/daemon/webapp.go`

## P0

### 1. Hook Trust Assistant [DONE 2026-06-18]

Problem:
- Users can install hooks successfully and still hit an agent-side trust/review prompt with no Onibi guidance.
- Codex surfaced the risk directly: Onibi wrote metadata fields that Codex rejected at startup.

Product requirement:
- After `onibi install-hooks --agent codex`, print:
  - `Codex next step: run codex, choose Review hooks, inspect onibi-notify commands, then trust if they match.`
  - `Do not choose Trust all unless you have inspected every command.`
  - `Continue without trusting disables these Onibi hooks for that Codex run.`
- Add `onibi hooks show --agent codex`.
- `hooks show` must print:
  - provider config path
  - Onibi DB hook record path/hash/version
  - backup path if a backup exists
  - expected event names
  - expected `onibi-notify` command per event
  - installed command per event
  - drift status: missing, extra, changed, schema-invalid, hash-missing, hash-mismatch, outdated, ok
  - manual trust instructions for providers with user trust gates
- Do not write Codex trust state.

Implementation:
- Add `hooksCmd()` subcommands:
  - `onibi hooks show --agent <name>`
  - `onibi hooks show --all`
  - `onibi hooks show --agent <name> --json`
- Add adapter interface extension:
  - `ExpectedHooks(notifyBin string) ([]ExpectedHook, error)`
  - `ObservedHooks() ([]ObservedHook, error)`
  - `TrustInstructions() []string`
  - `BackupPath() string`
- Keep existing `Status`, `Verify`, `Adopt`.
- Add a hook backup registry:
  - before install modifies a provider config, copy original file to an Onibi-owned backup path
  - record backup path in SQLite
  - never overwrite an existing backup for the same source hash
- Suggested backup path:
  - `$ONIBI_STATE_DIR/hook-backups/<agent>/<sha256-prefix>/<basename>`
- Add Codex-specific post-install output from `installOneAgent`.
- Keep output short on success; make `hooks show` the detailed path.

Acceptance:
- Running `onibi install-hooks --agent codex` prints the exact next step.
- Running `onibi hooks show --agent codex` prints all 4 Codex expected commands.
- Running `onibi hooks show --agent codex --json` is stable enough for support bundle use.
- Codex config contains no `onibiManaged` or `onibiIntegrationVersion` fields.
- A user-created hook in the same file is shown as "user hook, not managed" and is never deleted.
- Tests cover fresh install, reinstall, legacy metadata migration, user hook preservation, missing backup, and tamper drift.

Files:
- `internal/cli/hooks.go`
- `internal/adapters/*/install.go`
- `internal/adapters/common/common.go`
- `internal/store/*`
- `internal/doctor/doctor.go`

### 2. Provider Schema Strictness Audit [DONE 2026-06-18]

Problem:
- Codex was strict and rejected unknown top-level fields.
- Other providers may become stricter or already reject unknown fields in specific surfaces. [Inference]
- Current adapters still write Onibi metadata into provider config for Claude, Gemini, Goose, and Copilot.

Action:
- For every adapter, verify provider docs and runtime behavior for unknown fields.
- Remove unknown metadata fields from provider-owned JSON unless verified safe and intentional.
- Store version/managed state using:
  - SQLite hook records
  - `ONIBI_INTEGRATION_VERSION=...` in command env when command hooks are supported
  - generated plugin constants only inside Onibi-owned plugin/extension source files
  - shell comments only inside Onibi-managed shell blocks
- Add schema regression tests per adapter using the documented provider schema.
- Add `onibi doctor --after-upgrade` check that catches provider config fields likely to break startup.

Adapter-specific audit:
- Codex:
  - Current patched strategy is correct direction: no unknown provider fields, version in command env.
  - Need `hooks show` and backup flow.
- Claude:
  - Current matcher entries include `onibi-managed` and `onibiIntegrationVersion`.
  - Verify whether Claude Code accepts unknown fields in hook matcher entries.
  - If not verified, migrate to command env/DB-only metadata.
  - Add `/hooks` guidance in `hooks show`.
- Gemini:
  - Docs list hook config fields as `type`, `command`, `name`, `timeout`, `description`.
  - Current adapter writes `onibiManaged` and `onibiIntegrationVersion` in hook entries plus top-level version.
  - Migrate metadata out unless runtime/docs verify those fields are accepted.
  - P0 bug candidate: Gemini docs specify `timeout` in milliseconds, but adapter uses `30` and `360`.
- Goose:
  - Open Plugins example does not show Onibi metadata fields.
  - Current adapter writes top-level and hook-level metadata.
  - Migrate metadata out unless Open Plugins spec explicitly allows extension keys.
- Copilot:
  - Docs list command hook fields: `type`, `bash`, `powershell`, `command`, `cwd`, `env`, `timeout`, `timeoutSec`.
  - Current adapter writes `onibiManaged` and `onibiIntegrationVersion`.
  - Migrate metadata out unless runtime/docs verify unknown fields are accepted.
- OpenCode/Amp/Pi:
  - Generated source files are Onibi-owned plugin/extension files, so constants are acceptable.
  - Still add runtime reload instructions and schema/API contract tests.
- Shell:
  - Onibi-owned comment block is acceptable.
  - Keep block markers stable and explicit.

Acceptance:
- `rg -n "onibiManaged|onibiIntegrationVersion" ~/.codex ~/.gemini ~/.agents ~/.copilot ~/.claude` has no hits in provider-owned JSON unless that adapter has a verified exception documented in code.
- `go test -race -count=1 ./internal/adapters/... ./internal/doctor` covers migration from legacy metadata.
- `onibi doctor --after-upgrade` reports stale/strict-schema risk before the user opens Codex/Gemini/Copilot/Claude.

### 3. Doctor As A Repair Plan [DONE 2026-06-18]

Problem:
- Doctor checks are useful but too diagnostic-only.
- Failures should tell users impact and next action without requiring source knowledge.

Product requirement:
- Normalize every non-PASS check into:
  - `impact`
  - `safe fix`
  - `manual fix`
  - `files touched`
  - `retry command`
  - `blocks`
- Keep terse default text.
- Add `--json` for structured support and UI.
- Add `--explain` for verbose repair plans.

Keychain timeout requirement:
- If bot token lookup times out:
  - impact: Telegram daemon startup, approvals, notifications, and Telegram-created sessions cannot work until token lookup succeeds.
  - still works: existing local shells/agents outside Onibi and any already-running PTY/tmux process keep running at OS level. [Inference]
  - retry command: `onibi doctor`, `onibi up`, or `onibi run`.
  - safe fix: unlock/login to OS keychain, then rerun retry command.
  - manual fix: rotate token only if the stored token is invalid or inaccessible after keychain repair.
  - files touched: none for retry; `.env` only if user explicitly chooses dotenv fallback.

Implementation:
- Extend `doctor.Check`:
  - `Impact string`
  - `SafeFix string`
  - `ManualFix string`
  - `FilesTouched []string`
  - `Retry string`
  - `Blocks []string`
  - `Code string`
- Replace `nextAction()` string-only mapping with table-driven repair specs.
- Keep old `Next` for compatibility until downstream formatting migrates.
- Add output modes:
  - default: one-line check plus `next=...`
  - `--explain`: include impact/fix/files
  - `--json`: full struct
- Add repair plans for:
  - state dir perms
  - `.env` fallback perms/presence
  - sqlite missing/corrupt
  - secrets backend open failure
  - bot token missing/timeout
  - owner missing
  - socket missing/not listening
  - service missing/stopped
  - tmux missing
  - terminal launcher missing
  - hook none/missing/hash missing/tampered/outdated/schema risk
  - TOTP missing
  - Telegram 2FA ack missing
  - Telegram reachability/getUpdates/webhook/bot identity
  - encrypted mode seed missing
  - Mini App URL invalid/unreachable

Acceptance:
- Every FAIL/WARN has impact, safe fix, manual fix, files touched.
- `go test -race -count=1 ./internal/doctor` verifies keychain timeout formatting.
- `onibi doctor --json | jq` is valid.
- `onibi doctor --after-upgrade` runs offline-safe schema/hash/migration checks by default.

Files:
- `internal/doctor/doctor.go`
- `internal/cli/doctor.go`
- `internal/secrets/*`
- `docs/troubleshooting.md`

### 4. Telegram Home Dashboard [DONE 2026-06-18]

Problem:
- `/menu` exists, but behaves like a command hub, not a state dashboard.
- Phone users need current state plus next actions in one screen.

Product requirement:
- Make `/menu` the primary UI.
- One screen must show:
  - daemon state
  - default target
  - sessions
  - pending approvals
  - queued prompts
  - snooze
  - secure mode
  - hook health summary
- Inline buttons must map to common actions.
- Raw commands remain available as advanced/diagnostic paths.

Telegram-backed UI pattern:
- Telegram supports bot command menus, inline keyboards, callback buttons, reply keyboards, and Web Apps.
- Use command menu for discovery.
- Use inline keyboards for stateful actions where no chat text should be emitted.
- Use Web App/Mini App for encrypted controls and multi-field forms.

Implementation:
- Replace current menu text with a compact dashboard card:
  - `Onibi`
  - `daemon: up/down/degraded`
  - `target: <name> (<id>) or none`
  - `sessions: N total, B busy, H headless, V visible`
  - `approvals: N pending`
  - `queue: N queued`
  - `snooze: off/global/agent`
  - `secure: off/ask/on, seed ok/missing, mini app ok/missing`
  - `hooks: ok/warn/fail`
- Keyboard rows:
  - row 1: `Status`, `Sessions`, `Queue`, `Secure`
  - row 2: `New Visible`, `New Headless`, `Projects`
  - row 3 per active session: `<name>` target selector
  - row 4 per selected/default session: `Peek`, `Send`, `Interrupt`, `Show/Hide`
  - row 5 diagnostics: `Doctor`, `Hooks`
- Add callbacks:
  - `menu_projects`
  - `menu_doctor`
  - `menu_hooks`
  - `menu_send`
  - `menu_snooze`
  - `menu_unsnooze`
- Add stable short session labels.
- Keep callback_data under Telegram's 64-byte limit.

Acceptance:
- `/menu` without active sessions is still useful: shows setup/new-session actions.
- `/menu` with one session lets user target, peek, queue/send, interrupt, show/hide.
- `/menu` with multiple sessions makes default target obvious.
- `/menu` in encrypted mode blocks plaintext prompt paths and offers `/secure`.
- Tests assert menu text contains daemon/default/sessions/approvals/queue/snooze/secure/hook health.

Files:
- `internal/daemon/commands.go`
- `internal/daemon/controls.go`
- `internal/telegram/keyboards.go`
- `internal/telegram/commands.go`
- `internal/daemon/commands_test.go`

### 5. Approval Message Polish [DONE 2026-06-18]

Problem:
- Approval messages show core details but not enough context for daily risk decisions.
- Deny is one-tap only, with no optional reason flow.
- Edit validation exists, but coverage is Claude-shaped.

Product requirement:
- Each approval should show:
  - agent
  - session name/id
  - project/cwd
  - tool
  - risk
  - exact command or file target
  - TTL/expires time
  - buttons
- For Edit:
  - validate JSON before sending to provider
  - validate provider-specific edited input shape
  - explain validation errors in Telegram
- For Deny:
  - one-tap deny still works
  - add optional deny reason path
  - provider receives reason when supplied
- [Inference] This is the highest-risk daily workflow because it gates tool execution from a phone.

Implementation:
- Extend intake event normalization to extract:
  - `Agent`
  - `ProviderSessionID`
  - `Session`
  - `CWD`
  - `Tool`
  - `ToolTarget`
  - `Command`
  - `FilePath`
  - `Risk`
  - `ExpiresAt`
- Add provider-specific render extractors:
  - Claude: Bash command, file_path, grep/glob pattern, web URL, todo summary.
  - Codex: Bash/apply_patch command and MCP tool args.
  - Gemini: `tool_name`, `tool_input`.
  - Copilot: camelCase and VS Code-compatible payloads.
  - OpenCode/Amp/Pi: plugin event input fields.
- Add `Deny` and `Deny with reason` buttons:
  - Current `Deny` remains immediate.
  - New `Reason` parks pending text, same pattern as Edit.
  - Encrypted mode uses Mini App text field.
- Add TTL display:
  - `expires: 21:44:03 local (4m58s)`
  - update original approval keyboard/message after terminal decision when possible.
- Improve risk text:
  - `low`, `medium`, `high`
  - reasons: destructive shell, writes outside cwd, secrets path, network, git rewrite, package publish, credential file, sudo, rm, chmod/chown, production-looking target.
- Do not over-truncate target line.
- Attach full payload only when needed.

Acceptance:
- Approval tests cover command extraction and file target extraction per provider.
- Deny reason path stores reason and returns it to hook response.
- Edit path rejects invalid JSON and provider-invalid shape.
- TTL is visible in plaintext and encrypted approval paths.
- High-risk double-confirm remains.

Files:
- `internal/intake/*`
- `internal/daemon/approvals.go`
- `internal/approval/*`
- `internal/daemon/webapp.go`
- `docs/miniapp/index.html`

### 6. First-Run Guided Path [DONE 2026-06-18]

Problem:
- Docs describe the path, but the product does not drive it.

Product requirement:
- After setup, Onibi offers:
  - `Add project`
  - `Choose agent`
  - `Start visible session`
  - `Trigger test approval`

Implementation:
- Add setup completion message:
  - CLI prints next action.
  - Telegram sends onboarding dashboard if owner is paired.
- Add `onibi first-run` or `onibi demo approval`:
  - creates a local fake approval through the same queue/render path
  - clearly labels it as test
  - does not execute provider tools
- Add Telegram onboarding callbacks:
  - `Add project`: prompt for alias/path or open Mini App form
  - `Choose agent`: list detected agents
  - `Start visible`: prefill `/new --visible --project <alias> <agent>`
  - `Test approval`: send fake approval
- Use project alias picker; raw path entry is advanced.

Acceptance:
- New user can complete setup and see one Telegram approval without reading docs.
- Onboarding does not require hook trust until the user chooses an agent that needs hooks.
- Onboarding can be dismissed and reopened with `/menu`.

Files:
- `internal/cli/setup.go`
- `internal/daemon/commands.go`
- `internal/telegram/keyboards.go`
- `internal/daemon/approvals.go`
- `docs/getting-started.md`

## P1

### Session Cards [DONE 2026-06-18]

Requirement:
- Replace flat session lines with cards:
  - stable name
  - short id
  - age
  - cwd/project alias
  - agent
  - visible/headless
  - busy/idle/ended
  - last activity
  - queued prompts
  - pending approvals

Action:
- Add `SessionCard` formatter shared by `/sessions`, `/menu`, and encrypted Mini App.
- Persist last activity timestamp per session.
- Show cwd as project alias when possible.
- Avoid raw full path unless advanced/detail view.

Acceptance:
- Session labels do not reorder randomly.
- Default target is obvious.
- Long names/path text stay readable on phone.

### Project Alias Picker [DONE 2026-06-18]

Requirement:
- Phone users should not type raw paths unless advanced.

Action:
- Add `/project add` guided flow:
  - CLI can pre-seed current repo: `onibi project add here`
  - Telegram lists aliases as buttons
  - `/new` callback uses selected alias and selected agent
- Add alias health:
  - exists/missing
  - writable/read-only
  - git repo yes/no
  - trusted by agent if provider exposes trust state

Acceptance:
- New visible/headless session can be started from buttons after one alias exists.
- Missing alias path gives a repair message, not a silent launch failure.

### Queue UX [DONE 2026-06-18]

Requirement:
- Queue state should explain when a prompt is waiting behind an active turn.

Action:
- Queue card fields:
  - position
  - target
  - created age
  - first line preview
  - state: queued, sending, sent, cancelled, failed
  - reason: queued behind active turn, no default target, session ended
- Buttons:
  - Send now
  - Edit
  - Cancel
  - Up
  - Down
  - Move top
  - Flush
- Add send-now confirm when session is busy.

Acceptance:
- User can reorder without typing IDs.
- Sending into busy session requires explicit confirm.

### Encrypted Mode Readiness [DONE 2026-06-18]

Requirement:
- Secure mode should show readiness, not just on/off.

Readiness fields:
- seed present
- devices paired
- Mini App URL set
- Mini App URL HTTPS or localhost/dev
- WebApp action roundtrip last seen
- plaintext commands blocked when mode is `on`
- `/secure` button available

Action:
- Add doctor checks:
  - seed missing
  - Mini App URL missing
  - Mini App URL not HTTPS outside localhost
  - encrypted mode on but Telegram plaintext path accepted
- Add `/secure status`.
- Add one-tap `/secure` route from plaintext denial messages.

Acceptance:
- `telegram.encrypted_mode=on` with missing seed fails doctor with repair plan.
- Plaintext `/prompt`, `/send`, `/editprompt`, `/rename` remain blocked in encrypted mode.

### Hook Compatibility Matrix [DONE 2026-06-18]

Requirement:
- Users and support need a single view of hook status across providers.

Action:
- Add `onibi hooks matrix`.
- Columns:
  - provider
  - support: blocking/event-bridge/plugin/shell
  - install path
  - observed version
  - bundled version
  - trusted/manual step
  - config schema status
  - hash status
  - drift
  - next action
- Include shells.

Acceptance:
- Matrix explains why Goose is event-bridge while Codex/Claude/Gemini/Copilot/OpenCode/Amp/Pi can block or intercept tool calls where supported.

### Redacted Support Bundle [DONE 2026-06-18]

Requirement:
- Add `onibi support-bundle --redacted`.

Contents:
- `doctor --json --offline`
- hook matrix JSON
- adapter expected/observed hooks
- version/build info
- OS/shell summary
- recent Onibi logs
- recent audit entries with secrets removed
- config keys with sensitive values redacted
- encrypted mode readiness, no seed
- database schema version, no data dump

Redaction:
- bot token
- owner chat id by default, unless `--include-chat-id`
- prompt text
- approval input payloads
- file contents
- secrets/env values
- absolute home path optionally replaced with `~`

Acceptance:
- Bundle can be attached to a GitHub issue without leaking tokens or prompt bodies.
- Tests run redaction fixtures.

### Upgrade Smoke [DONE 2026-06-18]

Requirement:
- Add `onibi doctor --after-upgrade`.

Checks:
- third-party hook schemas
- legacy metadata fields
- hook command path exists
- `onibi-notify` version matches `onibi`
- DB migrations complete
- config parse
- encrypted seed present if encrypted mode on
- Mini App URL present if encrypted mode on
- provider-specific startup risks:
  - Codex unknown fields/trust pending
  - Gemini timeout unit mismatch
  - Copilot unknown fields
  - Claude unknown fields
  - plugin source stale for OpenCode/Amp/Pi

Acceptance:
- Upgrade smoke catches the Codex `onibiIntegrationVersion` class of issue before user opens Codex.

## Adapter Hook Matrix

### Codex [DONE 2026-06-18]

Provider surface:
- `~/.codex/hooks.json`
- top-level `hooks`
- command hook fields include `type`, `command`, `timeout`, `statusMessage`
- non-managed command hooks require user review/trust

Current Onibi:
- `SessionStart` -> `agent_message`
- `PreToolUse` -> blocking `approval_request`
- `PostToolUse` -> `agent_message`
- `Stop` -> `agent_done`
- version stored in command env after Codex schema fix

Needed:
- `hooks show --agent codex`
- post-install trust instructions
- backup display
- trust-state language: Onibi can show expected commands, but Codex owns trust
- schema test for no unknown fields
- document Codex PreToolUse limitations: not all tool paths are intercepted

### Claude Code [DONE 2026-06-18]

Provider surface:
- `~/.claude/settings.json`
- hooks configured under `hooks`
- `/hooks` inside Claude Code can browse configured hooks
- `PreToolUse` supports `permissionDecision`, `permissionDecisionReason`, and `updatedInput`

Current Onibi:
- `Stop` -> `agent_done`
- `PreToolUse` -> blocking `approval_request`
- Claude-shaped edit validation exists.

Needed:
- Verify unknown metadata fields in matcher entries.
- Add or consider:
  - `SessionStart` -> `agent_message`
  - `UserPromptSubmit` -> audit/context event
  - `PostToolUse` -> `agent_message`
  - `PostToolUseFailure` -> `agent_message` with failure marker
  - `SessionEnd` -> `session_exited`
- Preserve `Stop` as turn complete.
- Add `hooks show --agent claude` with `/hooks` instruction.
- Add provider version/runtime smoke if Claude exposes version command.

### Gemini CLI [DONE 2026-06-18]

Provider surface:
- `~/.gemini/settings.json`
- hooks under `hooks`
- command hooks use JSON stdin/stdout/stderr
- docs list timeout in milliseconds
- `BeforeTool` can deny or rewrite tool input via stdout JSON

Current Onibi:
- `SessionStart` -> `agent_message`
- `BeforeAgent` -> `agent_message`
- `BeforeTool` -> blocking `approval_request`
- `AfterTool` -> `agent_message`
- `Notification` -> `agent_message`
- `AfterAgent` -> `agent_done`
- `SessionEnd` -> `session_exited`

Needed:
- P0 verify/fix timeout units. Current `30`/`360` likely means 30ms/360ms against docs. [Inference]
- Migrate metadata out of provider JSON unless verified safe.
- Add tests for `BeforeTool` deny and edited input output format.
- Consider `AfterTool` redaction support for sensitive output.
- Consider `PreCompress` event for session summary notification.
- Do not add `BeforeModel`/`AfterModel` until there is a product need; those are deeper model-context hooks.

### Goose [DONE 2026-06-18]

Provider surface:
- Open Plugins style folder under `~/.agents/plugins/<name>/`
- `hooks/hooks.json`
- supported events include `SessionStart`, `SessionEnd`, `Stop`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `BeforeReadFile`, `AfterFileEdit`, `BeforeShellExecution`, `AfterShellExecution`
- docs state hook failures/timeouts are logged and do not crash the host tool

Current Onibi:
- `SessionStart` -> `agent_message`
- `UserPromptSubmit` -> `agent_message`
- `PreToolUse` -> `agent_message`
- `PostToolUse` -> `agent_message`
- `PostToolUseFailure` -> `agent_message`
- `Stop` -> `agent_done`
- support marked `event-bridge`

Needed:
- Keep Goose as event-bridge unless blocking semantics are verified.
- Add `SessionEnd` -> `session_exited`.
- Add granular events:
  - `BeforeReadFile` -> `agent_message`
  - `AfterFileEdit` -> `agent_message`
  - `BeforeShellExecution` -> `agent_message`
  - `AfterShellExecution` -> `agent_message`
- Migrate metadata out of provider JSON unless Open Plugins allows it.
- Add `hooks show --agent goose`.

### OpenCode [DONE 2026-06-18]

Provider surface:
- plugin file under OpenCode config plugin directory
- plugin exports functions returning hooks
- documented events include `tool.execute.before`, `tool.execute.after`, `session.idle`, session events, shell events, TUI events
- throwing in `tool.execute.before` can block a tool in examples

Current Onibi:
- generated `onibi.js`
- generic `event` handler -> `agent_message`
- `tool.execute.before` -> `agent_message`, then blocking `approval_request`
- `tool.execute.after` -> `agent_message`
- `session.idle` -> `agent_done`

Needed:
- Add plugin reload instruction after install.
- Verify config path is current: repo code uses `~/.config/opencode/plugins/onibi.js`; docs examples use `.opencode/plugins/...`.
- Add project-local vs user-global install choice.
- Add session lifecycle events if available:
  - session created/status/error/deleted -> message/exited
- Validate edited input JSON before assigning `output.args`.
- Add plugin API compatibility tests with a fixture.

### Amp [DONE 2026-06-18]

Provider surface:
- TypeScript plugin file
- events: `session.start`, `agent.start`, `tool.call`, `tool.result`, `agent.end`
- `tool.call` handler is a request event and should return an action object such as `allow`, `reject-and-continue`, `modify`, `synthesize`, or `error`

Current Onibi:
- generated `onibi.ts`
- `session.start` -> `agent_message`
- `agent.start` -> `agent_message`
- `tool.call` -> `agent_message`, then blocking `approval_request`
- `tool.result` -> `agent_message`
- `agent.end` -> `agent_done`
- current deny/expired path throws an error

Needed:
- P0 verify/fix deny path to return `{ action: "reject-and-continue", message }` instead of throwing if Amp treats throws as plugin errors.
- For edited input, return `{ action: "modify", input }` instead of mutating `event.input` if that is the documented contract.
- Add plugin reload instruction: command palette `plugins: reload`; `plugins: list` for verification.
- Add generated plugin type tests or snapshot tests.
- Add `hooks show --agent amp` that prints plugin path and reload instruction.

### Pi [DONE 2026-06-18]

Provider surface:
- TypeScript extension
- global auto-discovery path documented as `~/.pi/agent/extensions/`
- project-local path documented as `.pi/extensions/`
- extension can subscribe to lifecycle events, block/modify tool calls, register tools, commands, UI
- `tool_call` can block; mutations to `event.input` affect execution; no re-validation after mutation

Current Onibi:
- generated `onibi.ts`
- `session_start` -> `agent_message`
- `agent_start` -> `agent_message`
- `input` -> `agent_message`
- `tool_call` -> `agent_message`, then blocking `approval_request`
- edited input mutates `event.input`
- `tool_result` -> `agent_message`
- `agent_end` -> `agent_done`
- `session_shutdown` -> `session_exited`

Needed:
- Add `/reload` instruction after install.
- Add project-local vs user-global install choice.
- Add explicit warning in code comments/tests that Pi does no re-validation after mutation; Onibi must validate before mutation.
- Add provider-specific edited input shape tests.
- Add `hooks show --agent pi`.

### GitHub Copilot CLI [DONE 2026-06-18]

Provider surface:
- JSON hook files with `"version": 1`
- user-level hooks: `~/.copilot/hooks/*.json` or `$COPILOT_HOME/hooks/*.json`
- command hook fields include `bash`, `powershell`, `command`, `cwd`, `env`, `timeout`, `timeoutSec`, `type`
- events include `sessionStart`, `userPromptSubmitted`, `preToolUse`, `postToolUse`, `postToolUseFailure`, `notification`, `agentStop`, `sessionEnd`, `errorOccurred`, plus more
- `preToolUse` can return `permissionDecision`, `permissionDecisionReason`, and `modifiedArgs`
- `timeoutSec` is seconds

Current Onibi:
- writes one hook file
- `sessionStart` -> `agent_message`
- `userPromptSubmitted` -> `agent_message`
- `preToolUse` -> blocking `approval_request`
- `postToolUse` -> `agent_message`
- `postToolUseFailure` -> `agent_message`
- `notification` -> `agent_message`
- `agentStop` -> `agent_done`
- `sessionEnd` -> `session_exited`
- `errorOccurred` -> `agent_message`

Needed:
- Migrate Onibi metadata out of Copilot hook JSON unless unknown fields are verified safe.
- Verify file location: code writes `~/.copilot/hooks/onibi.json`, docs load `*.json` under that directory.
- Add `permissionRequest` hook if product needs pre-permission interception in CLI.
- Add tests for modified args path.
- Add `disableAllHooks` detection in status/doctor.
- Add `hooks show --agent copilot`.

### Shell Hooks [DONE 2026-06-18]

Provider surface:
- zsh: `.zshrc` with `add-zsh-hook preexec/precmd`
- bash: `.bashrc` with `DEBUG` trap and `PROMPT_COMMAND`
- fish: conf.d file with `fish_preexec`/`fish_postexec`

Current Onibi:
- long-command `cmd_done` notification after minimum duration.

Needed:
- Add shell hook preview to `hooks show --shell zsh|bash|fish`.
- Add compatibility notes:
  - oh-my-zsh/plugin order
  - bash existing `PROMPT_COMMAND`
  - fish conf.d ordering
- Add backup path.
- Add opt-in threshold display and edit command.

## User Flow Map

### Install From Source [DONE 2026-06-18]

Flow:
1. `git clone`
2. `make install`
3. `onibi up`
4. setup starts if not paired
5. doctor runs
6. optional hooks install
7. first-run guided path

Failure branches:
- Go/toolchain missing
- `onibi-notify` not installed
- state dir perms wrong
- Keychain unavailable
- Telegram token missing
- owner not paired
- service install fails
- tmux missing
- hook provider not installed
- Codex requires trust review

Needed:
- Setup must end with the next useful action, not a generic success.
- Doctor repair plan must explain which failures block Telegram vs only optional features.

### Bot Creation And Pairing [DONE 2026-06-18]

Flow:
1. user creates Telegram bot through BotFather
2. Onibi stores token in keychain or `.env`
3. Onibi generates pairing token/deep link
4. owner opens Telegram link
5. Onibi stores owner chat id
6. Onibi registers command menu

Failure branches:
- token typo
- bot already has webhook
- another getUpdates poller
- owner opens expired token
- owner is wrong account
- Telegram 2FA not acknowledged

Needed:
- Pairing screen should include `/menu` next step.
- Doctor should clearly say whether bot token retry is safe.

### Hook Install And Trust [DONE 2026-06-18]

Flow:
1. user runs `onibi install-hooks --agent <agent>`
2. Onibi backs up provider config
3. Onibi writes hook config/plugin
4. Onibi records hook hash
5. Onibi prints provider-specific next step
6. user opens agent
7. agent loads hooks
8. user reviews/trusts if provider requires it

Failure branches:
- provider config parse error
- unknown schema fields
- stale Onibi binary path
- hook trust prompt ignored
- hook hash missing
- hook tampered
- hook timed out

Needed:
- `hooks show` before/after install.
- `doctor --after-upgrade`.
- provider-specific trust/reload instructions.

### Start Session [DONE 2026-06-18]

Flow:
1. user chooses project alias
2. user chooses agent
3. user chooses visible or headless
4. daemon starts tmux-backed session
5. Onibi marks default target
6. `/menu` shows session card

Failure branches:
- no project alias
- raw path invalid
- agent binary missing
- tmux missing
- terminal launcher missing
- visible terminal cannot open
- daemon down

Needed:
- Project alias picker.
- Start visible defaults for first run.
- Clear headless vs visible labels.

### Approval [DONE 2026-06-18]

Flow:
1. agent calls tool
2. provider hook sends approval request
3. Onibi maps provider payload to normalized approval
4. Telegram shows approval card
5. user approves, denies, edits, or times out
6. provider receives decision
7. original Telegram message is marked decided

Failure branches:
- daemon down
- Telegram send fails
- approval expires
- duplicate callback
- edit JSON invalid
- edit schema invalid
- deny reason omitted
- encrypted mode missing seed
- high-risk approval needs confirm

Needed:
- richer approval card.
- optional deny reason.
- provider-specific target extraction.
- encrypted parity.

### Prompt Queue [DONE 2026-06-18]

Flow:
1. user sends prompt
2. Onibi resolves target
3. if busy, prompt queues
4. queue card shows position
5. user edits/reorders/cancels/sends now
6. prompt dispatches after turn complete

Failure branches:
- no target
- multiple targets
- session ended before dispatch
- encrypted mode rejects plaintext
- busy session send-now needs confirm

Needed:
- queue state reasons.
- reorder buttons.
- send-now confirm.

### Encrypted Mode [DONE 2026-06-18]

Flow:
1. user enables encrypted mode
2. setup creates seed
3. user pairs Telegram device/Mini App
4. Onibi sends encrypted approvals/output/prompts
5. Mini App decrypts and sends encrypted actions

Failure branches:
- seed missing
- Mini App URL missing
- Mini App unavailable
- URL too long, document fallback needed
- user tries plaintext prompt
- user switches devices without seed

Needed:
- readiness state.
- one-tap `/secure`.
- doctor repair plan.

### Upgrade

Flow:
1. user upgrades binary
2. hook schemas may be stale
3. provider may parse config before user can repair

Needed:
- post-install formula/cask should suggest `onibi doctor --after-upgrade`.
- `doctor --after-upgrade` should catch schema drift and stale binary paths.
- `hooks show --all` should compare bundled vs installed versions.

### Uninstall

Flow:
1. user runs `onibi uninstall`
2. service stops
3. hooks removed
4. state optionally retained or deleted

Failure branches:
- hook file missing
- provider config parse error
- user hooks in same file
- backup restore requested

Needed:
- `onibi hooks show` before uninstall.
- `onibi hooks restore --agent <agent> --backup <id>` as P2 if backups prove useful.

## Release Gates

- `go test -race -count=1 ./...`
- `onibi doctor --offline --json` valid on a fresh dev state.
- `onibi doctor --after-upgrade --offline` catches legacy Codex metadata fixture.
- `onibi hooks show --agent codex --json` valid.
- Adapter fixtures parse without unknown-field warnings where providers expose validators.
- Manual smoke:
  - Codex fresh open after hook install shows review prompt, not parse warning.
  - Claude `/hooks` shows Onibi hooks.
  - Gemini hook install has correct timeout units.
  - Goose starts with plugin present.
  - OpenCode plugin reloads.
  - Amp plugin reloads and deny path behaves as intended.
  - Pi `/reload` loads extension.
  - Copilot hook file loads with no schema warning.
  - Telegram `/menu` shows dashboard.
  - Approval card shows agent/session/project/tool/risk/target/TTL.
  - Encrypted `/secure` path works.

## Explicit Non-Goals

- No auto-trusting Codex hooks.
- No hidden provider config writes.
- No broad adapter expansion before setup, trust, doctor, and approval UX are reliable.
- No plaintext fallback when encrypted mode is `on`.
- No raw path typing as the primary phone path.
