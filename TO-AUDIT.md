1. Poller/token conflict needs a hard product flow.
   Current state: getUpdates conflicts are detected, persisted, surfaced by
   doctor, and `rotate-token` exists.
   Still open: one bot token should have one active owner daemon with an
   explicit takeover/lease/rotate path, not just detection plus operator action.
   Refs: internal/telegram/client.go, internal/telegram/probe.go,
   internal/doctor/doctor.go, internal/setup/rotate.go

2. /screenshot is not a real Ghostty/window screenshot.
   Current state: it renders the PTY ring buffer through a vt100 replay PNG.
   Still open: decide whether product semantics should remain "terminal render"
   or add an optional macOS/Ghostty window capture path. If kept as vt100 render,
   rename/document it to avoid user confusion.
   Refs: internal/render/png.go, internal/daemon/controls.go,
   internal/telegram/commands.go, README.md

3. Docs still lag the current session-control model.
   Current state: README command list covers visible/headless sessions,
   projects, show, and hide.
   Still open: docs/architecture.md should document the visible/headless tmux
   model, session restore/reconcile behavior, project aliases for Telegram
   starts, and the newer session control commands/events.
   Refs: docs/architecture.md, README.md, docs/getting-started.md
