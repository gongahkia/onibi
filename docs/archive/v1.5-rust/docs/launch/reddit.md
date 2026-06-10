# Reddit Draft

Primary target:

```text
r/programming
```

Title:

```text
Onibi: local-first approvals for multi-vendor coding agents
```

Body:

```text
I built Onibi, a local-first approval gate for AI coding agents.

The main workflow: run Claude Code, Codex, OpenCode, Qoder, Copilot CLI, Goose, or another local agent, let it work, and when it pauses before a tool call, Onibi sends the approval to your phone. You can approve, deny, edit the command, or send audited literal text into a selected Onibi pane before the agent continues. Claude Code is the strongest path through updatedInput; Codex is Bash-only; several other adapters now have blocking hook/event integrations.

It is Rust/Tauri plus a mobile PWA, runs as a desktop app or headless daemon, and supports Tailscale Funnel, Cloudflare Quick Tunnel, and LAN HTTPS.

If your whole workflow is Claude-only and a vendor-native remote-control feature already solves it, Onibi may be overkill. The project is aimed at local-first multi-vendor gating, edit-before-approve, and headless/LAN/Pi setups.

Repo: https://github.com/gongahkia/onibi
Demo GIF: https://github.com/gongahkia/onibi/blob/main/asset/screencast/onibi-launch.gif
Architecture: https://github.com/gongahkia/onibi/blob/main/docs/architecture.png

I would appreciate feedback on the protocol and whether this approach fits your agent workflow.
```

Optional target:

```text
r/LocalLLaMA or r/MachineLearning only if framed as local tooling, not a research result.
```

Notes:

- Do not post until binaries and install instructions are live.
- Avoid duplicate blasting; space posts out and answer comments.
