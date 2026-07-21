# Product Scope

Onibi is a personal, self-hosted, single-owner local coding-agent host with a paired phone web cockpit. The local OS user and their paired owner browsers are the trust boundary; Onibi has no planned tenant, team, invitation, delegated-operator, or shared-control workflow.

The supported path is a managed local terminal session, HTTPS pairing, WebSocket terminal I/O, and owner-approved Claude Code hooks. Ghostty handover is macOS-only.

Not part of the focused product:

- Hosted, remote, or multi-user control planes.
- Third-party control bridges or credential-management surfaces by default. Experimental Telegram and IRC are explicit opt-ins and remain non-certified until their separate live evidence is recorded.
- Secondary product surfaces outside local terminal pairing and Web Push.
- Non-Ghostty desktop handover claims.

Release work focuses on the local cockpit, pairing security, approval correctness, Web Push, macOS distribution, and documented install verification.
