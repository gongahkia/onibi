# Reddit Draft

Primary target:

```text
r/programming
```

Title:

```text
Onibi: approve local AI coding agent actions from your phone
```

Body:

```text
I built Onibi, a local-first approval cockpit for AI coding agents.

The main workflow: run Claude Code or another local agent, let it work, and when it pauses before a tool call, Onibi sends the approval to your phone. You can approve, deny, or edit the command before the agent continues. The lead adapter uses Claude Code's updatedInput hook path; Codex has Bash-only interception; the rest of the launch adapters are mirrored shell sessions until their hook APIs are stable enough.

It is Rust/Tauri plus a mobile PWA, runs as a desktop app or headless daemon, and supports Tailscale Funnel, Cloudflare Quick Tunnel, and LAN HTTPS.

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
