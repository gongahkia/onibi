# Email Handoff Drafts

Piranesi can generate a local `.eml` draft for report delivery. The command does
not send email. It creates subject/body text and references local report artifacts
so an operator can review the draft before using an email client.

```bash
piranesi integrations email-handoff \
  --workspace ./workspace \
  --to client@example.com \
  --artifact reports/red-team-handoff-archive.zip \
  --json
```

Defaults:

- Output is written to `workspace/reports/email-handoff-draft.eml`.
- A delivery manifest is written next to the draft at
  `workspace/reports/email-handoff-manifest.json`.
- Red-team handoff summary counts are used for the email body.
- Sensitive evidence content and raw artifacts are not embedded in the draft.
- Existing files in `workspace/reports/` are referenced when `--artifact` is not
  provided.
- Automatic sending is not implemented and requires a separate approval gate.

Artifacts must stay inside the workspace so the draft cannot accidentally point at
unrelated local files.

The manifest records the draft path, draft digest, referenced artifact paths,
artifact digests, recipients, subject, generated timestamp, and `sent: false`.
It is local review metadata, not proof that an email was sent.
