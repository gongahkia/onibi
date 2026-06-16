  1. Stale session state is real.
     onibi sessions --all currently reports old PTY sessions as active even with no
     live tmux server. DB active rows are not reconciled on daemon startup.
     Refs: internal/store/sessions.go:54, internal/daemon/daemon.go:364

  2. Tmux sessions are not restored after daemon restart.
     The DB persists tmux_target, but startup does not re-register live tmux sessions
     into memory. Visible/headless semantics need this.
     Refs: internal/daemon/tmux.go:67, internal/daemon/registry.go:92

  3. MCP is likely broken by the new managed-session gate.
     MCP sends approval/notify events without Managed=true; daemon rejects unmanaged
     session events. Tests mock intake directly, so they miss daemon behavior.
     Refs: internal/mcpserver/server.go:134, internal/daemon/daemon.go:448

  4. Telegram /new starts in daemon cwd, usually state dir.
     CLI onibi new captures caller cwd; Telegram cannot pick repo cwd yet. This
     caused the Codex “trust this directory” behavior.
     Refs: internal/daemon/commands.go:437, internal/cli/session_control.go:29

  5. Global hooks still execute in every external agent process.
     They fail open now, but still spawn onibi-notify globally. [Inference] Add hook-
     level env guards to reduce blast radius and regression risk.
     Ref: internal/adapters/common/common.go:69

  6. Approval rendering can fail or become unreadable for large patches.
     Telegram messages have size limits; approval bodies are not split/truncated, and
     Go JSON escaping turns HTML patches into \u003c.... Large apply_patch approval
     UX is currently poor.
     Refs: internal/daemon/approvals.go:514, internal/daemon/approvals.go:540

  7. Tmux prompt queue semantics bypass queue.
     Test asserts tmux prompts send immediately. That is inconsistent with “queue
     when busy” UX and can inject mid-turn.
     Ref: internal/daemon/tmux_test.go:165

  8. Poller/token conflict handling is too soft.
     getUpdates conflicts are warnings. For product, one bot token should have one
     active owner daemon, with a takeover/rotate flow.
     Ref: internal/telegram/client.go:232

  9. /screenshot is not a Ghostty screenshot.
     It is a fixed 80x24 vt100 replay. ANSI/256/truecolor SGR is partly supported,
     but not full terminal fidelity.
     Ref: internal/render/png.go:34

  10. Docs drift.
     Architecture omits session_new/show/hide; README quickstart still leads with
     onibi run, not the intended Telegram visible/headless flow.
     Refs: docs/architecture.md:109, README.md:56
