# Onibi

[![CI](https://github.com/gongahkia/onibi/actions/workflows/ci.yml/badge.svg?branch=v1.5)](https://github.com/gongahkia/onibi/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

Onibi -- cross-platform cockpit for local AI coding agents, with a phone in your pocket.

Status: 🚧 v1.5 in development.

Track progress in the repo-root `PHASE-XX-PLAN-27-MAY.md` files.

v1.5 rebuilds Onibi as a Tauri desktop app backed by Rust and a system webview, plus a mobile PWA for approval inboxes and live terminal mirror.

The architecture target is desktop-first control with phone-side review: local agents pause, Onibi surfaces context, and the phone becomes the second screen for approve, deny, and edit flows. The final diagram lands in [PHASE-07-PLAN-27-MAY.md](PHASE-07-PLAN-27-MAY.md).

Onibi is not the coding agent.

It is the harness around local agents.

Differentiation:

- vs cmux: cross-platform desktop plus mobile second-screen.
- vs Aider/Codex/Claude Code: Onibi orchestrates and reviews; it does not replace them.
- vs hosted relays: local-first, self-hosted transports are first-class.

Roadmap:

- [PHASE-00](PHASE-00-PLAN-27-MAY.md): foundation and demolition.
- [PHASE-01](PHASE-01-PLAN-27-MAY.md): PTY core and xterm.
- [PHASE-02](PHASE-02-PLAN-27-MAY.md): agent surfaces.
- [PHASE-03](PHASE-03-PLAN-27-MAY.md): protocol spec.
- [PHASE-04](PHASE-04-PLAN-27-MAY.md): mobile PWA.
- [PHASE-05](PHASE-05-PLAN-27-MAY.md): transports.
- [PHASE-06](PHASE-06-PLAN-27-MAY.md): packaging.
- [PHASE-07](PHASE-07-PLAN-27-MAY.md): launch polish.

License: Apache-2.0.
