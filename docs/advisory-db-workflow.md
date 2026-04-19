# Advisory DB Workflow

Piranesi supports a first-class local advisory database workflow through `piranesi advisory` commands.
Network activity is explicit and only occurs when you run `piranesi advisory update`.

## Commands

- `piranesi advisory status` - inspect local DB metadata (version/source/last update/checksum/freshness).
- `piranesi advisory update` - sync advisory feeds into the local DB.
- `piranesi advisory sign-snapshot <path>` - generate detached snapshot manifest (optionally signed).
- `piranesi advisory import <path>` - import a DB snapshot (offline-friendly).
- `piranesi advisory search` - query advisories by text/ecosystem/package.

## Online Update Flow

```bash
uv run piranesi advisory update --project-root .
uv run piranesi advisory status --project-root .
```

Optional source targeting:

```bash
uv run piranesi advisory update \
  --project-root . \
  --source osv --source ghsa \
  --ecosystem npm
```

## Offline Flow

When outbound network is restricted, use an imported snapshot:

```bash
uv run piranesi advisory sign-snapshot ./artifacts/advisory.db \
  --manifest ./artifacts/advisory.db.manifest.json \
  --key-file ./trust/advisory.key \
  --signer security-team

uv run piranesi advisory import ./artifacts/advisory.db \
  --manifest ./artifacts/advisory.db.manifest.json \
  --trust-key ./trust/advisory.key \
  --require-verified-snapshot \
  --trust-policy verified-only \
  --on-unsigned fail \
  --project-root .

uv run piranesi advisory status --project-root .
```

Reports include advisory DB freshness metadata and warnings (missing/empty/stale DB),
so offline runs remain deterministic and auditable.

## Trust Policy Controls

All advisory commands support deterministic trust-policy enforcement:

- `--trust-policy permissive|verified-only`
- `--on-missing ignore|warn|fail`
- `--on-stale ignore|warn|fail`
- `--on-unsigned ignore|warn|fail`

Example strict status check:

```bash
uv run piranesi advisory status \
  --project-root . \
  --trust-policy verified-only \
  --on-stale fail \
  --on-unsigned fail
```

Example strict import gate:

```bash
uv run piranesi advisory import ./artifacts/advisory.db \
  --manifest ./artifacts/advisory.db.manifest.json \
  --trust-key ./trust/advisory.key \
  --require-manifest \
  --require-verified-snapshot \
  --trust-policy verified-only \
  --on-unsigned fail \
  --project-root .
```

## Search Examples

```bash
uv run piranesi advisory search --project-root . --query prototype
uv run piranesi advisory search --project-root . --ecosystem npm --package lodash
```

## Freshness Metadata

`advisory status` reports:

- schema version (`PRAGMA user_version`)
- advisory and package counts
- synced source list
- last updated timestamp
- DB checksum (`sha256`)
- freshness state (`fresh`, `stale`, `empty`, `missing`) and warnings
- trust state (`verified`, `unsigned`, `unverified`, `unknown`)
- snapshot provenance metadata (`signer`, `manifest digest`, `import timestamp`)
