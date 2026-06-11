 Fix First

  1. Release path is not done: live provider e2e, Apple cert, hardened
     runtime, signed checksums, Homebrew tap, Linux installer, clean Mac
     test remain. TODO-10-JUN.md:491, TODO-10-JUN.md:501

  Polish

  Web Notes

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
  - Supply-chain: SBOM, signed checksums, reproducible build job.
