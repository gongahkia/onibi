 Fix First

  1. Release path is not done: live provider e2e, Apple cert, hardened
     runtime, signed checksums, Homebrew tap, Linux installer, clean Mac
     test remain. TODO-10-JUN.md:491, TODO-10-JUN.md:501

  Polish

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
