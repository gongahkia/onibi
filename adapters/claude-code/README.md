# Claude Code hook templates

Source-of-truth templates installed by `onibi install-hooks --agent claude`
into `~/.claude/settings.json` as managed command hooks.

Inspect with:

```sh
onibi hooks show --agent claude
```

`hooks show` prints the settings path, Onibi's recorded hash/version, latest backup path if present, expected commands, installed commands, drift, and `/hooks` review guidance.

Events used:

- `SessionStart` — session start/resume; emits `agent_message`.
- `UserPromptSubmit` — prompt audit/context; emits `agent_message`.
- `Stop` — turn complete; emits `agent_done` event.
- `PreToolUse` — blocking approval; emits `approval_request` event and waits
  for the daemon's decision (`updatedInput` for edits, exit 2 for deny).
- `PostToolUse` — successful tool event; emits `agent_message`.
- `PostToolUseFailure` — failed tool event; emits `agent_message` with the provider event name.
- `SessionEnd` — session exit; emits `session_exited`.

Trust state:

- Onibi can show expected commands and drift.
- Claude Code owns hook browsing; run Claude Code and open `/hooks` to inspect configured hooks.

Hook script body invokes `onibi-notify` which talks to the daemon over the
local Unix socket. If the daemon is down the hook exits 0 silently — never
blocks the user's session.

Hashes recorded in the SQLite `hooks` table on install; `onibi doctor`
verifies hook integrity.
