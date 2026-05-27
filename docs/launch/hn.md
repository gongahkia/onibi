# Show HN Draft

Title:

```text
Show HN: Onibi - Approve your local AI coding agent from your phone
```

URL:

```text
https://github.com/gongahkia/onibi
```

Body/comment:

```text
Hi HN, I built Onibi: a local-first cockpit for AI coding agents that lets you approve, deny, or edit tool calls from your phone.

The demo is the whole idea. Claude Code proposes something like `rm -rf tests/legacy`, Onibi blocks the hook, your phone buzzes, and you can change it to `mv tests/legacy tests/legacy.bak` before approving. The lead adapter uses Claude Code's `updatedInput` hook path; Codex has Bash-only interception; other agents can run as mirrored shell sessions while their hook surfaces mature.

The app is Rust/Tauri plus a mobile PWA. It runs as a desktop app or a headless daemon, supports macOS/Linux/Pi, and can publish the phone UI over Tailscale Funnel, Cloudflare Quick Tunnel, or LAN HTTPS with a self-signed cert.

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
