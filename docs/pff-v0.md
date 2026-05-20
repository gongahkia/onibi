# Piranesi Finding Format v0

Piranesi Finding Format (PFF) v0 is the first public interchange shape for findings, assets,
evidence, provenance, retest state, and chain-of-custody references. The JSON Schema lives at
[`docs/schemas/pff-v0.schema.json`](schemas/pff-v0.schema.json).

Validate a PFF document with the bundled schema:

```bash
uv run piranesi pff validate --input findings.pff.json
```

Export workspace findings to PFF:

```bash
uv run piranesi pff export --workspace ./workspace --output findings.pff.json
```

The v0 schema is intentionally close to the current normalized finding model so current adapter
findings can be represented without known information loss. It is additive-first: future changes
should add optional fields until a separately documented migration policy exists.
Versioning rules are documented in [`docs/pff-versioning.md`](pff-versioning.md).

Top-level fields:

- `schema_version`: `piranesi.pff.v0`.
- `producer`: emitter name and version.
- `engagement`: local workspace engagement metadata.
- `findings`: normalized findings with assets, service, evidence, source references, provenance,
  retest status, and optional chain-of-custody references.
- `known_gaps`: explicit limitations for an export instead of hidden adapter loss.

Known v0 boundaries:

- PFF v0 is a schema draft, not a plugin execution API.
- PFF v0 does not define adapter packaging, registry trust, or marketplace semantics.
- `chain_of_custody` is nullable until the export workflow links finding-level entries to signed
  manifests.
