# Final QA Checklist

Date: 2026-05-27.

## Exit Criteria

| # | Criterion | Status |
| ---: | --- | --- |
| 1 | README rewritten, screencast embedded, install one-liners verified | Partial: README and media done; clean external install verification pending release binaries |
| 2 | Blog post published | Pending: draft is in `docs/blog/2026-06-03-launch.md` |
| 3 | HN Show post live | Pending: draft is in `docs/launch/hn.md` |
| 4 | X thread within 30 minutes of HN | Pending: draft is in `docs/launch/x-thread.md` |
| 5 | `v1.5.0` tag exists; release binaries available | Pending PR approval/merge/tag |
| 6 | `cargo audit` and `pnpm audit` clean | Passed for high/critical CVEs; cargo audit still reports advisory warnings for transitive unmaintained crates |
| 7 | `onibi doctor` ships and runs on all three platforms | Partial: command runs locally; Linux/Pi verification pending |
| 8 | Security, adapter, transport, and architecture docs present | Done |
| 9 | No P0 open at launch hour | Pending launch hour triage |

## Automated Checks

See `docs/dry-run-log.md` for command outputs and local dry-run notes.

## Manual Gates Before Tag

- Reconcile branch state: local Phase 06 changes exist outside the Phase 07 allow-list, and `origin/v1.5` is behind the integrated `main` branch in this checkout.
- PR reviewed and merged into `v1.5`.
- Clean macOS user account install.
- Clean Ubuntu 22.04 VM install.
- Raspberry Pi 5 arm64 install.
- Tailscale Funnel phone-on-LTE test.
- Cloudflare Quick Tunnel phone-on-LTE test.
- LAN HTTPS with trusted cert on a real phone.
- External friend verifies README install path from scratch.

Do not tag `v1.5.0` until these gates are green or explicitly waived.
