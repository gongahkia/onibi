# Advisory Air-Gapped Trusted Workflow

## Goal
Move advisory snapshots from a connected sync environment into an air-gapped scan environment with explicit integrity and trust checks.

## Connected Environment (Build Snapshot)

1. Sync feeds and produce snapshot:
```bash
uv run piranesi advisory update --project-root . --db ./dist/advisory.db
```

2. Sign detached manifest:
```bash
uv run piranesi advisory sign-snapshot ./dist/advisory.db \
  --manifest ./dist/advisory.db.manifest.json \
  --key-file ./trust/advisory.key \
  --signer security-team
```

3. Transfer only:
- `advisory.db`
- `advisory.db.manifest.json`
- trusted verification key (`advisory.key`) via your approved secret channel

## Air-Gapped Environment (Verify + Import)

1. Import with strict verification:
```bash
uv run piranesi advisory import ./incoming/advisory.db \
  --manifest ./incoming/advisory.db.manifest.json \
  --trust-key ./trust/advisory.key \
  --require-manifest \
  --require-verified-snapshot \
  --trust-policy verified-only \
  --on-missing fail \
  --on-stale fail \
  --on-unsigned fail \
  --project-root .
```

2. Assert policy compliance:
```bash
uv run piranesi advisory status \
  --project-root . \
  --trust-policy verified-only \
  --on-stale fail \
  --on-unsigned fail \
  --json
```

## Operational Notes

1. `verified-only` mode requires advisory `trust_state=verified`.
2. Manifest digest mismatch or signature mismatch causes import rejection when `--require-verified-snapshot` is set.
3. Provisions are persisted in DB provenance metadata so status/report artifacts remain auditable offline.
