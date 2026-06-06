# Show HN Draft

Title:

```text
Show HN: Onibi - local-first approvals for multi-vendor coding agents
```

URL:

```text
https://github.com/gongahkia/onibi
```

Body/comment:

```text
Hi HN, I built Onibi: a local-first approval gate for AI coding agents. It blocks local tool calls, mirrors them to the desktop and a mobile PWA, and lets you approve, deny, or edit the payload before the agent continues.

The demo is the whole idea. An agent proposes something like `rm -rf tests/legacy`, Onibi blocks the hook, your phone buzzes, and you can change it to `mv tests/legacy tests/legacy.bak` before approving. Claude Code is the strongest path through `updatedInput`; Codex is Bash-only; OpenCode, Qoder, Copilot CLI, and Goose also have blocking hook/event integrations.

The app is Rust/Tauri plus a mobile PWA. It runs as a desktop app or a headless daemon, supports macOS/Linux/Pi, and can publish the phone UI over Tailscale Funnel, Cloudflare Quick Tunnel, or LAN HTTPS with a self-signed cert.

If you only use Claude and Anthropic's native remote-control flow covers your needs, use that. Onibi is for the cases that still need local-first multi-vendor gating, edited tool input, headless/Pi operation, or LAN-only control.

Repo: https://github.com/gongahkia/onibi
Demo GIF: https://github.com/gongahkia/onibi/blob/main/asset/screencast/onibi-launch.gif
Architecture: https://github.com/gongahkia/onibi/blob/main/docs/architecture.png

I am around for questions and especially interested in feedback on the approval protocol and adapter model.
```

Timing:

- Lead channel.
- Target Tuesday or Wednesday US morning.
- Do not post until the repo has release binaries and users can try it without asking for access.

HN guideline check:

- Show HN must be something people can run or try.
- Avoid posting only the blog post.
- Do not ask anyone to upvote or comment.

Reference: <https://news.ycombinator.com/showhn.html>
