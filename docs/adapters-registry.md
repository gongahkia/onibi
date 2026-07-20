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
| Aider post-edit event example | [`examples/aider-adapter/aider.toml`](../examples/aider-adapter/aider.toml) | Non-certified, non-enforcing post-edit test event only; it has no native tool-approval boundary. |

## Official Adapter Deny Verification

| Adapter | Hook surface | Verified deny blocks tool | Test | Notes |
|---|---|---:|---|---|
| Amp | `tool.call` plugin approval | Yes | `TestAdapterAmpDenyBlocksTool` | Deny returns `reject-and-continue`; fixture writes only if the hook allows. |
| Claude | `PreToolUse` command hook | Yes | `TestAdapterClaudeDenyBlocksTool` | Deny exits non-zero with `permissionDecision=deny`. |
| Codex | `PreToolUse` command hook | Yes | `TestAdapterCodexDenyBlocksTool` | Deny exits non-zero with `permissionDecision=deny`. |
| Copilot | `preToolUse` command hook | Yes | `TestAdapterCopilotDenyBlocksTool` | Deny emits provider JSON `permissionDecision=deny`; fixture writes only if provider output allows. |
| Gemini | `BeforeTool` command hook | Yes | `TestAdapterGeminiDenyBlocksTool` | Deny emits provider JSON `decision=deny`; fixture writes only if provider output allows. |
| OpenCode | `tool.execute.before` plugin hook | Yes | `TestAdapterOpenCodeDenyBlocksTool` | Deny throws before fixture write. |
| Pi | `tool_call` extension hook | Yes | `TestAdapterPiDenyBlocksTool` | Deny returns `{ block: true }`; fixture writes only if the hook allows. |
| Goose | `PreToolUse` event bridge | No - notify-only | `TestAdapterGooseDenyNotifyOnly` | Current adapter sends `agent_message`; it does not wait for an approval decision. |

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
