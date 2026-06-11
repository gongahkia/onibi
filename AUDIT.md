 Fix First

  1. Edit-before-approve is partial: JSON syntax check exists, but no
     per-tool schema validation, no paranoid TOTP, no original-vs-edited
     diff hash. internal/daemon/approvals.go:236, internal/approval/
     queue.go:227

  2. Telegram hardening remains: startup deletes webhook, but does not
     alert if one existed; getUpdates 409 is treated as OK. internal/
     telegram/client.go:94, internal/telegram/probe.go:61

  3. TODO is stale/inconsistent: §14 still says Phase 3 “ready to
     start”, while later code exists; setMyCommands is checked in Phase
     10 but unchecked in polish. TODO-10-JUN.md:522, TODO-10-JUN.md:539,
     TODO-10-JUN.md:696

  4. Release path is not done: live provider e2e, Apple cert, hardened
     runtime, signed checksums, Homebrew tap, Linux installer, clean Mac
     test remain. TODO-10-JUN.md:491, TODO-10-JUN.md:501

  5. Tmux backend is still TODO. TODO-10-JUN.md:475

  Polish

  - Quote Claude hook commands; current string concat can break paths
    with spaces. internal/adapters/claude/install.go:186

  - Bump golang.org/x/sys/x/term; govulncheck found no reachable vuln,
    but x/sys has a module-level Windows vuln fixed after current
    version.

  - Make docs/security match implementation; current docs imply stronger
    TOTP/paranoid/edit guarantees than code enforces.

  - Add a release smoke target: snapshot artifact, checksum, local
    install, doctor.

  - Add bot token redaction around raw Telegram HTTP errors. [Inference]
    rawBotCall builds token-bearing URLs and returns raw transport
    errors. internal/telegram/probe.go:73

  Web Notes

  - Codex PreToolUse is a guardrail, not a complete boundary; it
    intercepts Bash/apply_patch/MCP, but not all shell or non-shell
    tools. Document adapter limits exactly. (developers.openai.com
    (https://developers.openai.com/codex/hooks))

  - Codex hook discovery via hooks.json/config.toml and plugin hooks
    matches the adapter direction. (developers.openai.com
    (https://developers.openai.com/codex/hooks))

  - Telegram long polling cannot work while webhook is set;
    deleteWebhook/getWebhookInfo checks are correctly on-theme, but
    alerting is unfinished. (core.telegram.org
    (https://core.telegram.org/bots/api)) (core.telegram.org
    (https://core.telegram.org/bots/api))

  - macOS distribution still needs real signing/notarization validation;
    Apple and GoReleaser docs back keeping this as a launch gate.
    (developer.apple.com
    (https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution?utm_source=openai))
    (goreleaser.com
    (https://goreleaser.com/customization/sign/notarize/?utm_source=openai))

  Expansion Aligned

  - Generic “wrap any TUI over PTY” mode.
  - MCP adapter as the broadest agent surface.
  - Multi-machine routing with labels.
  - Offline voice input via local Whisper.
  - Per-tool risk classifier for rm, force-push, secret-looking args.
  - Local observability: --debug, rotating logs, tail-log, audit export.
  - Supply-chain: SBOM, signed checksums, reproducible build job.
