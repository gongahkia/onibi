# Community Adapter Registry

This file is the adapter registry. There is no hosted registry service, package index, telemetry feed, or automatic remote install path.

Adapters listed here are maintained by their authors. Onibi only provides the manifest format and local CLI checks:

```sh
onibi adapters validate ./adapter.toml
onibi adapters add ./adapter.toml
```

## Official Examples

| Adapter | Manifest | Notes |
|---|---|---|
| Aider example | [`examples/aider-adapter/aider.toml`](../examples/aider-adapter/aider.toml) | Demonstrates a third-party manifest and Aider config helper scripts. |

## Community Adapters

No community adapters are listed yet.

To add one, open a PR that adds one row:

```markdown
| Name | Manifest URL | Author | Notes |
```

Required evidence:

- Public repository link.
- Direct link to the adapter TOML.
- `onibi adapters validate <path>` output in the PR.
- Minimum supported Onibi version.
- Short explanation of what hook surface the provider exposes.

The manifest URL must be HTTPS. Users installing from a URL must pin the manifest hash:

```sh
onibi adapters add https://example.com/adapter.toml --sha256=<64-hex>
```
