# Aider Post-Edit Event Example (Non-Certified)

This example is non-certified and non-enforcing. It reports configured post-edit test completion; it does not intercept, inspect, approve, deny, or edit Aider tool actions.

Evidence reviewed on 2026-07-20: Aider documents `.aider.conf.yml`, `--test-cmd`, and `--auto-test` after edits:

- https://aider.chat/docs/config/aider_conf.html
- https://aider.chat/docs/usage/lint-test.html

No documented API in those sources provides synchronous pre-tool interception or approval-decision mapping. The generated config therefore installs one post-edit event helper only:

- `~/.local/bin/onibi-aider-event`: fire-and-forget Onibi event for Aider test runs.

It does not satisfy the [certified adapter contract](../../docs/adapter-contract.md), and must not be presented as approval enforcement.

## Install

```sh
onibi adapters validate ./examples/aider-adapter/aider.toml
onibi adapters add ./examples/aider-adapter/aider.toml
onibi install-hooks --agent aider
```

The install step writes:

```text
~/.config/onibi/aider/aider.onibi.conf.yml
~/.local/bin/onibi-aider-event
```

Use the generated Aider config explicitly:

```sh
aider --config ~/.config/onibi/aider/aider.onibi.conf.yml
```

## Uninstall

```sh
onibi install-hooks --agent aider --uninstall
```
