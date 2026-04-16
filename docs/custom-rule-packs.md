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
- source/sink pattern sections
- sanitizer pattern sections
- explanatory message templates
- receiver-constrained sink patterns where relevant

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
