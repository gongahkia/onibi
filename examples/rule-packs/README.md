# Example Rule Packs

These rule packs are first-party learning examples for custom rule authoring.
They are intentionally conservative and are **not production-ready coverage claims**.

## Included packs

- `node-express/open-redirect.toml` (`CWE-601`) - Express redirect sink with receiver constraints.
- `python-flask/ssti.toml` (`CWE-94`) - Flask `render_template_string` sink.
- `go-nethttp/header-injection.toml` (`CWE-113`) - net/http response header setter sink.
- `php-laravel/sql-injection.toml` (`CWE-89`) - Laravel-style raw SQL sinks.
- `ruby-rails/command-injection.toml` (`CWE-78`) - Rails params flowing into shell APIs.

## Validate the examples

```bash
uv run piranesi rules validate examples/rule-packs
```

## Enable an example pack

Example (`piranesi.toml`):

```toml
[rules]
paths = [
  "./rules",
  "examples/rule-packs/node-express",
  "~/.piranesi/rules/*",
]
```

## Copy and customize

```bash
mkdir -p rules/custom
cp examples/rule-packs/node-express/open-redirect.toml rules/custom/open-redirect.toml
uv run piranesi rules validate rules/custom/open-redirect.toml
```

Suggested customization flow:

1. Tighten `source` and `sink` patterns for your framework conventions.
2. Expand sanitizer patterns with project-specific escaping/validation helpers.
3. Adjust severity, tags, and message templates to match your triage workflow.
4. Add inline `[[tests]]` fixtures before enabling in CI.
