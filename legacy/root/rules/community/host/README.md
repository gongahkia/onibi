# Community Host Rules

This directory contains constrained, data-only host posture rules. Rules are TOML
documents parsed by `piranesi host rule test`; they cannot import Python, execute
shell commands, or run arbitrary scripts.

Use:

```bash
piranesi host rule scaffold "Disable risky service"
piranesi host rule test rules/community/host/my-rule.toml tests/fixtures/host/my-fixture
piranesi host rule test-all rules/community/host
```

Keep `index.json` updated with rule ID, maintainer, platform support, fixture
coverage, and last validation date.

See `docs/community-contribution-workflow.md` for the full contributor and
maintainer checklist.
