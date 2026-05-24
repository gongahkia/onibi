# Custom Rule Pack Examples

Piranesi ships first-party **example** custom rule packs under `examples/rule-packs/`.
These are intended for authoring guidance and adaptation, not complete production
coverage.

## Where to start

- [examples/rule-packs/README.md](../examples/rule-packs/README.md)
- `examples/rule-packs/node-express/open-redirect.toml`
- `examples/rule-packs/python-flask/ssti.toml`
- `examples/rule-packs/go-nethttp/header-injection.toml`
- `examples/rule-packs/php-laravel/sql-injection.toml`
- `examples/rule-packs/ruby-rails/command-injection.toml`

Each file demonstrates:

- rule metadata (`id`, `name`, `cwe_id`, `severity`, tags, author/version)
- optional schema metadata (`schema_version`) and `category`
- source/sink pattern sections
- sanitizer pattern sections
- explanatory message templates
- receiver-constrained sink patterns where relevant

## Create a new rule pack

Use the scaffold command to generate a starter pack with a rule file and test fixtures:

```bash
uv run piranesi rules scaffold "Payments SQL Rule Pack" --output ./rules
```

This creates:

- `rules/payments-sql-rule-pack/rules/payments-sql-rule-pack.toml`
- `rules/payments-sql-rule-pack/tests/fixtures/vulnerable.ts`
- `rules/payments-sql-rule-pack/tests/fixtures/safe.ts`

Then validate before running a full scan:

```bash
uv run piranesi rules validate rules/payments-sql-rule-pack/rules
```

## Enable a pack

Add an example pack directory to `rules.paths` in `piranesi.toml`:

```toml
[rules]
paths = [
  "./rules",
  "examples/rule-packs/node-express",
  "~/.piranesi/rules/*",
]
```

Then validate:

```bash
uv run piranesi rules validate examples/rule-packs/node-express
```

## Copy and customize

```bash
mkdir -p rules/custom
cp examples/rule-packs/python-flask/ssti.toml rules/custom/python-ssti.toml
uv run piranesi rules validate rules/custom/python-ssti.toml
```

Recommended edits before production use:

1. Narrow source/sink patterns to your codebase conventions.
2. Add project-specific sanitizers.
3. Add inline `[[tests]]` fixtures and run `piranesi rules test-all`.
4. Tune severity/message templates for your internal policy.

## Validate, Test, And Inspect Results

1. Validate syntax and schema constraints:
   `uv run piranesi rules validate <rule-file-or-dir>`
2. Run inline rule tests:
   `uv run piranesi rules test-all --rules-dir <rule-file-or-dir>`
3. Run a fixture scan for quick spot checks:
   `uv run piranesi rules test <rule-file-or-dir> --fixture <fixture-dir>`
4. Inspect emitted sink location and custom message in CLI output.

`rules validate` reports actionable errors with file/field context, including:

- malformed patterns (for example unbalanced CPGQL delimiters)
- duplicate rule IDs in a pack
- unknown fields in `[rule]` or nested sections
- unknown `rule.category` values
- unsupported `rule.schema_version` values

Currently supported `rule.schema_version` values: `1`, `1.0`.
Supported `rule.category` values: `authz`, `crypto`, `deserialization`,
`injection`, `misconfiguration`, `redirect`, `secrets`, `ssrf`,
`supply-chain`, `traversal`, `xss`, `other`.
