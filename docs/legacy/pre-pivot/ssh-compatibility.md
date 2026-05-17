> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# SSH Compatibility Matrix

Remote SSH collection is opt-in and environment-bound. CI continues to use fake
transports and fixture replay; live targets must never be committed.

## Current Matrix

| Distribution | Version | Sudo Mode | Trivy | Status | Caveats |
| --- | --- | --- | --- | --- | --- |
| Ubuntu | TBD | `never`, `passwordless` | default, `--no-trivy` | Not yet live-tested | Requires a disposable target. |
| Debian | TBD | `never`, `passwordless` | default, `--no-trivy` | Not yet live-tested | Requires a disposable target. |
| RHEL/Rocky/Fedora | TBD | `never`, `passwordless` | default, `--no-trivy` | Not yet live-tested | Requires a disposable target. |
| Amazon Linux | TBD | `never`, `passwordless` | default, `--no-trivy` | Not yet live-tested | Requires a disposable target. |
| Alpine | TBD | `never`, `passwordless` | `--no-trivy` first | Not yet live-tested | BusyBox command differences expected. |

## Live Test Protocol

For each disposable target:

```bash
piranesi remote collect --host <target> --output /tmp/piranesi-remote --no-trivy
piranesi remote collect --host <target> --output /tmp/piranesi-remote-trivy
piranesi assess /tmp/piranesi-remote --output /tmp/piranesi-remote-report --format both
```

Record:

- distribution and version;
- sudo mode;
- whether osquery, Trivy, package inventory, SSH config, listener, user, update,
  firewall, and command evidence were collected;
- expected warnings for unsupported tools;
- reassessment result path or partial-failure reason;
- redaction review outcome for SSH stderr and attempted command arrays.

Do not commit:

- hostnames;
- IP addresses;
- usernames;
- credentials;
- private keys;
- raw stderr containing infrastructure identifiers.

## CI Boundary

Normal CI remains local and deterministic:

```bash
uv run pytest tests/test_remote_collect.py tests/test_host_platforms.py
```

Live compatibility results should be summarized in this document only after
manual redaction.

