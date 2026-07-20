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
| Amp | `tool.call` plugin approval | Yes | `TestAdapterAmpDenyBlocksTool` | Deny returns `reject-and-continue`; edits return `modify` with object input. No `session.end` event exists; daemon-unavailable and timeout return `allow`. Certification remains pending authenticated live evidence and a provider-version floor. |
| Claude | `PreToolUse` command hook | Yes | `TestAdapterClaudeDenyBlocksTool` | Deny exits non-zero with `permissionDecision=deny`. |
| Codex | `PreToolUse` command hook | Yes | `TestAdapterCodexDenyBlocksTool` | Deny exits non-zero with `permissionDecision=deny`. |
| Copilot | `preToolUse` command hook | Yes | `TestAdapterCopilotDenyBlocksTool` | Deny emits provider JSON `permissionDecision=deny`; edited input maps to `modifiedArgs`. Copilot timeout and Onibi daemon/socket unavailability return no decision, while a missing notifier command fails closed; certification remains pending authenticated live evidence and a provider-version floor. |
| Gemini | `BeforeTool` command hook | Yes | `TestAdapterGeminiDenyBlocksTool` | Deny emits provider JSON `decision=deny`; fixture writes only if provider output allows. Native edits map to `hookSpecificOutput.tool_input`; certification remains pending authenticated live evidence and a provider-version floor. |
| OpenCode | `tool.execute.before` plugin hook | Yes | `TestAdapterOpenCodeDenyBlocksTool` | Deny throws before fixture write; approve, expiry, edit, timeout, daemon-unavailable, lifecycle, global/project scope, install drift, and reload instruction have provider-shaped local fixtures. `onibi adapters --json` exposes its non-certified contract v1 and documented OpenCode `1.18.3` floor. Authenticated live evidence is still required. |
| Pi | `tool_call` extension hook | Yes | `TestAdapterPiDenyBlocksTool` | Deny returns `{ block: true }`; fixture writes only if the hook allows. |
| Goose | `PreToolUse` native blocking hook | Yes - deny only | `TestAdapterGooseDenyBlocksTool` | Goose v1.35.0 added `PreToolUse` denial: exit 2 or `{"decision":"block"}` blocks the tool. Onibi reports this as its provider floor. Edited input is denied because Goose has no documented input-replacement response; no authenticated live evidence exists, so Goose remains non-certified. |

Goose documents [the native hook system and `PreToolUse` denial](https://github.com/aaif-goose/goose/releases/tag/v1.35.0). The floor is reported, not locally probed: verify the installed Goose version before relying on the hook.

OpenCode's [plugin documentation](https://opencode.ai/docs/plugins/) specifies global/project plugin discovery and mutable `tool.execute.before`; [v1.18.3](https://github.com/anomalyco/opencode/releases/tag/v1.18.3) is the reported documentation floor. It is not authenticated live evidence.

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
