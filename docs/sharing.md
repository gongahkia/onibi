# Viewer Sharing

Onibi viewer sharing mints a read-only pairing URL for one live session. It is for screen-share style review, not for delegation.

## Create a Viewer Link

```bash
./bin/onibi share <session-id>
```

Useful flags:

- `--ttl <duration>` sets how long the pairing URL can be claimed. Default: `1h`.
- `--max-viewers <n>` sets how many viewer devices can claim the URL. Default: `5`.
- `--copy` copies the primary URL to the clipboard on macOS.
- `--no-qr` prints URLs only.
- `--json` prints machine-readable URL metadata.

The link pairs the viewer into `/s/<session-id>`. Each claimed viewer device has role `viewer`.

## Threat Model

Viewers can see the live PTY stream. That includes command output, prompts, terminal scrollback sent through replay, editor contents visible in the terminal, file panel read views, snapshots, timeline details, and any secret printed while they are attached.

Viewers cannot steer the session through the current web control surface:

- PTY text and binary input frames from viewer sessions are dropped server-side.
- Resize frames are still accepted so the viewer terminal can fit the device.
- Owner-only HTTP controls such as `/control`, `/handover`, approval decisions, and file writes reject viewer sessions.
- The frontend hides owner-only toolbar actions and approval controls in viewer mode.

This is not a secret-redaction boundary. If a secret appears in the live PTY stream, the viewer can read it.

## Recommended TTLs

Use the shortest link lifetime that covers the review:

| situation | suggested TTL |
|---|---:|
| quick "look at this output" review | `5m` |
| active debugging with a trusted teammate | `15m` |
| longer walkthrough or handoff | `30m` |
| default local demo | `1h` |

Keep `--max-viewers` close to the expected audience. Use `--max-viewers 1` for one person.

## Anti-Patterns

- Do not share to an untrusted teammate while editing secrets. They see the secrets in the live PTY stream.
- Do not keep a viewer link open across unrelated work.
- Do not use viewer sharing as approval delegation. Viewers do not own approval decisions.
- Do not treat viewer sharing as audit-grade screen recording. Audit logs record attach and detach metadata, not a full transcript.

## Revoke Viewers

List paired devices and roles:

```bash
./bin/onibi devices
```

Revoke one viewer device:

```bash
./bin/onibi unpair --viewer <device-id>
```

Revoke all viewer devices:

```bash
./bin/onibi unpair --all-viewers
```

Viewer attach and detach events are written to audit as `viewer.attach` and `viewer.detach` with viewer ID, source IP, and user-agent.
