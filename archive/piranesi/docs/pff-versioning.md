# PFF Versioning

Piranesi Finding Format versions are identified by the top-level `schema_version` field. The
current version is `piranesi.pff.v0`.

## Compatibility Rules

- Readers must reject missing or unknown `schema_version` values.
- Writers should emit the latest supported version unless a caller explicitly requests an older
  supported version.
- Additive changes should prefer optional fields and schema defaults.
- Breaking changes require a new schema version and an explicit migration function.
- Migrations must validate the migrated document before returning it.

## Supported Versions

| Version | Status | Introduced | Migration |
| --- | --- | --- | --- |
| `piranesi.pff.v0` | Current | `0.2.0` | Initial public schema; no migration required. |

## Current Migration Behavior

The library exposes `migrate_pff_document()`. Because only v0 exists today, migration is currently an
identity operation for valid v0 documents and a deliberate error for unknown source or target
versions.
