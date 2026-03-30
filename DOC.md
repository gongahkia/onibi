# Onibi: Product, Architecture, and Operator Documentation

## 1) Repository Purpose and Intent

Onibi is a terminal-native operational layer for agent developers working in Ghostty. It is built around one core belief:

- Agent work should be observable as a first-class software system, not treated as opaque chat output.

In practical terms, Onibi turns shell-level command lifecycles and agent activity into structured events, notifications, logs, and remote-readable API payloads.

## 2) Philosophical Value Proposition

### Current posture

Before this iteration, Onibi already had strong ingredients:

- Shell hook ingestion for command start/end events.
- Session modeling for concurrent terminal workflows.
- Agent-kind classification (Claude Code, Codex, Gemini, Copilot, other AI).
- Local notifications and a companion mobile monitor.

### Core philosophical gap

The prior implementation had a trust gap for serious operators:

- Critical paths used silent fallbacks (`try?`) in storage, scheduling, and network services.
- Failures often became absence-of-signal instead of explicit diagnostics.
- Remote/mobile visibility was health-centric, not diagnostics-centric.

### Expanded posture after this work

Onibi now behaves more like an agent operations substrate:

- It records structured runtime diagnostics to a dedicated pipeline.
- It exposes diagnostics and assistant outcome analytics over the mobile API.
- It treats debuggability and rollbackability as product features, not implementation details.

## 3) Repository Structure (Functional Map)

- `Onibi/`
  - macOS host app runtime.
  - ingestion, scheduling, storage, notifications, shell integration, diagnostics.
- `OnibiCore/`
  - shared contracts for host and iOS client.
  - API models, routing contracts, client SDK, monitor view model.
- `OnibiPhone/`
  - iOS companion app using `OnibiCore` contracts.
- `OnibiTests/`, `OnibiCoreTests/`, `OnibiPhoneTests/`
  - unit and integration coverage for parsing, storage, lifecycle tracking, router behavior.
- `Package.swift`
  - SPM build for `Onibi` executable and `OnibiCore` library.
- `project.yml`
  - XcodeGen config for iOS app/test targets.

## 4) Critical Runtime Flow

1. Shell hooks append lifecycle lines to `~/.config/onibi/terminal.log`.
2. `BackgroundTaskScheduler` watches file changes and parses incremental lines.
3. Parsed events are classified and transformed into `GhosttyEvent`/`LogEntry`.
4. `JSONStorageManager` persists logs to JSON with backup support.
5. `EventBus` fan-outs updates to notification/UI view models.
6. `MobileGatewayService` serves authenticated host state to mobile clients.

## 5) What Was Expanded (Breadth + Depth)

### A) New diagnostics backbone

- Added `Onibi/Services/DiagnosticsStore.swift`.
- Captures structured events: timestamp, component, severity, message, metadata.
- Persists line-delimited JSON diagnostics to `~/.config/onibi/diagnostics.log`.
- Emits to `os.Logger` via new `Log.diagnostics` category.

### B) Silent-failure hardening across core services

Updated components now explicitly report operational failures with context:

- `Onibi/OnibiApp.swift`
  - startup directory-creation and deep-link failures now logged/diagnosed.
- `Onibi/Services/BackgroundTaskScheduler.swift`
  - startup, buffer setup, and flush failures surfaced with diagnostics + error reporting.
- `Onibi/Services/JSONStorageManager.swift`
  - directory creation, periodic flush, backup recovery, and atomic write edge cases instrumented.
- `Onibi/Services/MobileGatewayService.swift`
  - listener lifecycle, request parse failures, send failures, token errors, and tailscale enable failures tracked.
- `Onibi/Services/TailscaleServeService.swift`
  - command failure reasons now exposed in status detail instead of being dropped.
- `Onibi/Services/ShellHookInstaller.swift`
  - backup rotation and verification cleanup failure paths made explicit.
- `Onibi/Services/LogBuffer.swift`
  - file-handle and attribute failures now diagnosed.
- `Onibi/Services/NotificationManager.swift`
  - delivery, authorization, badge update, and action errors instrumented.
- `Onibi/ViewModels/NotificationViewModel.swift`
  - notification persistence encode/decode failures surfaced.
- `Onibi/ViewModels/SettingsViewModel.swift`
  - settings persistence and decode fallback paths instrumented.
- `Onibi/Services/DependencyContainer.swift`
  - dependency resolution failures and fatal DI misses now recorded.
- `Onibi/Services/GhosttyConfigParser.swift` and `Onibi/Services/GhosttyCliService.swift`
  - ghostty config/CLI fallbacks and command failures surfaced.
- `Onibi/Services/LogFileParser.swift`
  - regex compilation failures now tracked.
- `OnibiCore/Services/MobileConnectionStore.swift`
  - configuration decode/token-missing failures no longer silently swallowed.

### C) New diagnostics API surface

Added a host-facing diagnostics contract and route:

- New models in `OnibiCore/Models/MobileAPIModels.swift`:
  - `DiagnosticsSeverity`
  - `DiagnosticsEventPreview`
  - `DiagnosticsResponse`
- `OnibiCore/Services/MobileGatewayRouter.swift`
  - new `GET /api/v1/diagnostics`
  - improved error payload behavior for auth-provider and serialization failures.
- `OnibiCore/Services/MobileAPIClient.swift`
  - new `fetchDiagnostics()`.
- `OnibiCore/ViewModels/MobileMonitorViewModel.swift`
  - diagnostics included in refresh loop/state.
- `Onibi/Services/MobileGatewayService.swift`
  - host-side diagnostics aggregation implementation.

### D) Richer summary analytics for agent developers

`SummaryResponse` now includes:

- `assistantBreakdown`
- `successfulCommandCount`
- `failedCommandCount`

This enables quick assistant and reliability snapshots without full log export.

### E) Test contract updates

- Updated router and monitor tests to cover diagnostics contract changes.
- Added authorization provider failure behavior test for router.

## 6) API Endpoints (Host Gateway)

Authenticated via `Authorization: Bearer <pairing-token>`.

- `GET /api/v1/health`
- `GET /api/v1/summary`
- `GET /api/v1/sessions`
- `GET /api/v1/sessions/{id}`
- `GET /api/v1/events?cursor=<iso8601>&limit=<1..200>`
- `GET /api/v1/diagnostics`

## 7) Operational Debugging Playbook

### Primary local files

- `~/.config/onibi/terminal.log` (raw shell hook stream)
- `~/.config/onibi/logs.json` (structured command history)
- `~/.config/onibi/logs.backup.json` (storage recovery backup)
- `~/.config/onibi/error.log` (error reporter output)
- `~/.config/onibi/diagnostics.log` (structured runtime diagnostics)

### Fast triage sequence

1. Check `diagnostics.log` for recent `warning/error/critical` entries.
2. Inspect `error.log` for user-facing or critical failures.
3. Verify `summary` and `diagnostics` API responses from a client.
4. Validate shell hook installation status and log file writeability.
5. Validate gateway auth token and listener status if mobile path fails.

## 8) Developer Experience Rules for Future Changes

- Do not introduce silent `try?` in critical ingestion, persistence, auth, or network paths.
- Route operational failures through diagnostics with component + reason metadata.
- Reserve fatal errors for true unrecoverable misconfiguration.
- Keep API contracts additive and typed so mobile/remote clients stay robust.
- Preserve per-file atomic commits to support low-risk rollback.

## 9) Market Conventions Review (As of 2026-03-30)

This review was used to shape Onibi toward genuine agent-dev utility.

### Observed conventions

- Trace-first observability and debugging are baseline expectations.
- OpenTelemetry compatibility is becoming default for agent instrumentation portability.
- Evaluation and reliability loops (not just logs) are expected in production AI workflows.
- Terminal-native workflows still matter, especially when paired with Git-native rollback.
- API-first and self-hostable/local operation are preferred to reduce lock-in.

### Evidence (primary sources)

- Langfuse docs: open-source, self-hostable, API-first, tracing/evaluation/prompt lifecycle.
  - https://langfuse.com/docs
- OpenTelemetry GenAI semantic conventions (including agent spans, events, MCP-related conventions; stability opt-in mechanics).
  - https://opentelemetry.io/docs/specs/semconv/gen-ai/
  - https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/
  - https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-events/
- Arize Phoenix docs: open-source observability and evaluation built around OTEL/OpenInference patterns.
  - https://arize.com/docs/phoenix
- Promptfoo docs: test-driven eval and red-team workflow, local/CLI/CI usage model.
  - https://www.promptfoo.dev/docs/intro/
- Aider GitHub: terminal-native pair programming with automatic git commits.
  - https://github.com/Aider-AI/aider
- Claude Code CLI reference: MCP-capable CLI agent workflows and automation-friendly JSON output.
  - https://code.claude.com/docs/en/cli-usage
- OpenHands SDK observability docs: built-in OTEL tracing for agent execution/tool calls.
  - https://docs.openhands.dev/sdk/guides/observability

## 10) Strategic Positioning for Onibi

Onibi can occupy a distinct position if it remains:

- Terminal-first and low-friction for solo builders.
- Diagnostics-rich enough for serious debugging and postmortem analysis.
- API-contract stable enough to integrate into broader agent observability stacks.
- Local-first by default with optional remote/mobile visibility.

In short: Onibi should be the "agent operations lens" for Ghostty-native development, not just a notification surface.
