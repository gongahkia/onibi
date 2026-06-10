# Post-Launch Monitoring

Window: first 48 hours after the HN post.

## Hotfix Branch

Create the branch only if a launch-blocking issue appears:

```sh
git checkout v1.5
git pull --ff-only origin v1.5
git checkout -b v1.5.1-hotfix
```

Release flow:

```sh
cargo test --workspace
pnpm --filter onibi-app test
pnpm --filter onibi-mobile test
cargo audit
pnpm audit --prod
git tag -a v1.5.1 -m "Onibi v1.5.1 hotfix"
git push origin v1.5.1-hotfix v1.5.1
```

## Triage Criteria

Ship v1.5.1 only for:

- P0 crash on launch or daemon startup.
- Data loss or destructive command approval mismatch.
- Security issue involving token leakage, auth bypass, or remote unauthenticated access.
- Onboarding blocker that prevents a normal user from pairing or installing after following README commands.
- Release artifact problem where a tagged binary is missing or unusable for a launch target.

Defer to v1.6:

- UI polish.
- New adapter requests.
- Non-critical transport edge cases with a documented workaround.
- Feature requests for hosted relay, native mobile app, Windows, or team workflows.
- Docs wording unless it blocks installation.

## Monitoring Checklist

- Watch GitHub issues sorted by newest.
- Watch the HN thread for reproducible crashes and install failures.
- Keep a local clone on `v1.5` ready for cherry-picks.
- Keep `onibi doctor` output in every support reply.
- Ask reporters for OS, architecture, install path, transport, adapter, and redacted doctor output.
- Rotate demo tokens after posting screenshots or videos.

## Issue Labels

Use these labels during launch:

- `p0-launch`
- `security`
- `install`
- `transport`
- `adapter`
- `docs`
- `v1.6`

## First Response Template

```text
Thanks for the report. Could you paste the output of:

onibi doctor

Please redact bearer tokens or URLs if they appear. Also include OS/version, CPU architecture, install method, selected transport, and which agent adapter you were using.
```
