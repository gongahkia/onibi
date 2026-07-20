# Snapshots

Snapshots save enough session state to reopen a useful working context later. They are not VM checkpoints.

## CLI

```bash
onibi snapshot <session> <name>
onibi snapshots list
onibi restore <name>
onibi fork <name> @turn-N "new prompt"
onibi snapshots delete <name>
```

The phone cockpit has the same core flow: open `SNAP`, tap a snapshot to restore it in the current browser tab, or long-press a row to fork from a transcript turn.

## Stored State

A snapshot stores:

| field | behavior |
|---|---|
| PTY replay | Last captured terminal bytes, used to hydrate the visible pane. |
| cwd | Captured from the child process when possible, with session cwd fallback. |
| env | Captured from the child process when possible; restore drops `SSH_*` and `ONIBI_TOKEN_*`. |
| command | The original command is re-run through the user's shell. |
| Claude transcript offset | Current JSONL file size when the snapshot is taken. |

Snapshot rows are stored in SQLite with encrypted PTY replay, cwd, and env fields.

## Restore Model

Restore starts a new PTY in the captured cwd and runs the captured command again. It seeds the PTY replay buffer so the phone has visual context immediately.

This means restore is best-effort:

| app/workload | expected restore result |
|---|---|
| shell prompt | New shell in the captured cwd with restored env where available. In-memory shell state is gone. |
| `vim` / `nvim` | Reopens through the captured command. Swap/session files may recover editor state; unsaved memory-only edits are not a checkpoint. |
| `tmux attach` | Reattaches if the tmux session still exists. If tmux is gone, restore fails or starts whatever the command does now. |
| Claude Code | Starts from the captured command and points env at the known transcript path/offset. |
| `curl`, `ssh`, long-running network jobs | New process. Socket state, remote session state, and partial transfers are not resumed. |
| `npm install`, build jobs | New process. Existing files/cache may make rerun continue or no-op, but Onibi does not resume the old process. |
| REPLs (`python`, `node`, etc.) | New REPL. In-memory variables are lost. |

## Fork Model

Fork is transcript-first. For Claude Code snapshots with a located JSONL transcript, Onibi truncates the transcript to `@turn-N`, appends a new user prompt, updates the transcript offset, then restores the session command.

This is the reliable branching path because the fork source is durable text, not live process memory.

Rules:

- `@turn-N` is zero-based against non-empty JSONL lines.
- If `N` is past the end, Onibi appends the new prompt after the existing transcript.
- Empty prompts are rejected.
- Snapshots without a located transcript cannot fork.

## Limits

- Snapshots do not preserve process memory, file descriptors, sockets, process ids, job control, or kernel state.
- Restore may rerun side-effecting commands. Prefer snapshot names near a stable prompt, not mid-command.
- Environment capture is platform-dependent. Missing env falls back to the daemon process env on restore.
- PTY replay is visual context only. It does not imply the restored process is at the same prompt.
- Delete by name removes all snapshots with that name.

## Practical Pattern

Use snapshots before risky edits or before asking an agent to explore a branch:

```bash
onibi snapshot <session> before-refactor
onibi fork before-refactor @turn-12 "try the smaller parser design"
```

Use restore when you want the same cwd/command/terminal context back. Use fork when you want a Claude transcript branch.
