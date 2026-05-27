# Codex Adapter

The Phase-03 Codex adapter is intentionally Bash-only. It installs a command hook that runs `onibi _hook codex`, reads the hook JSON from stdin, blocks on `/v1/approval/request`, then writes a Codex-compatible permission decision to stdout.

Current limitation: `apply_patch` and non-Bash tools are not intercepted here.
