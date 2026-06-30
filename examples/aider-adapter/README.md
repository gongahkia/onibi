# Aider Adapter Example

This example shows how to package a third-party adapter manifest for Aider.

Official Aider docs support `.aider.conf.yml` config files and `--test-cmd` with `--auto-test` after edits:

- https://aider.chat/docs/config/aider_conf.html
- https://aider.chat/docs/usage/lint-test.html

No official Aider native `PreToolUse` hook surface was found in those docs. This example therefore installs two helper scripts:

- `~/.local/bin/onibi-aider-event`: fire-and-forget Onibi event for Aider test runs.
- `~/.local/bin/onibi-aider-approval`: blocking Onibi approval helper for custom wrappers or manual preflight checks.

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
~/.local/bin/onibi-aider-approval
```

Use the generated Aider config explicitly:

```sh
aider --config ~/.config/onibi/aider/aider.onibi.conf.yml
```

## Sample Approval Flow

Run Aider from an Onibi-managed terminal so `ONIBI_SESSION_ID` and `ONIBI_SOCK` are set.

For a custom preflight approval, pipe a normalized tool payload to the helper:

```sh
printf '%s\n' '{"tool_name":"Edit","input":{"file_path":"demo.py","new_string":"print(42)"}}' \
  | onibi-aider-approval
```

Onibi shows an approval card. Approve exits 0; deny exits non-zero through `onibi-notify`.

## Uninstall

```sh
onibi install-hooks --agent aider --uninstall
```
