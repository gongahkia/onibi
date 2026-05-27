# Lobste.rs Draft

Submission URL:

```text
https://github.com/gongahkia/onibi
```

Title:

```text
Onibi: phone approvals for local AI coding agents
```

Tags:

```text
show, rust, ai, mobile
```

Suggested comment:

```text
I built Onibi as a local-first approval cockpit for AI coding agents. It runs agents in a Tauri/Rust desktop app or a headless daemon, then mirrors approval requests to a mobile PWA over Tailscale Funnel, Cloudflare Quick Tunnel, or LAN HTTPS.

The main design choice is that approvals are protocol-level objects, not just notifications. Claude Code can return edited tool input through updatedInput, so the phone can change a proposed command and approve the edited version without adding another model turn. Codex has Bash-only interception; other launch adapters are honest shell/mirror integrations until their hook surfaces stabilize.

I would especially like feedback on the protocol shape and whether the transport split is the right tradeoff for local-first tools.
```

Timing:

- Submit after HN is live and the repository release is usable.
- Keep the first comment technical; Lobste.rs should get the protocol discussion, not marketing copy.
