# MCP setup

`kaypoh mcp serve` is a local stdio MCP server implemented with the official
[Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk). Stdio is the only
MCP transport exposed by this release. The separate local HTTP API is not an
MCP endpoint.

Read-only tools are always present:

- `kaypoh_search_availability`
- `kaypoh_list_venues`
- `kaypoh_list_sources`
- `kaypoh_list_watches`
- `kaypoh_list_events`

Set `mcp.allow_writes = true` to expose `kaypoh_create_watch`,
`kaypoh_add_manual_availability`, and `kaypoh_evaluate_watches`. These tools
remain local-only; they cannot book or read secrets. Source authentication, when
configured, occurs only during daemon/CLI refreshes and is not exposed through
MCP.

## Hermes

Hermes reads stdio MCP definitions from `mcp_servers` in
`~/.hermes/config.yaml`. Add:

```yaml
mcp_servers:
  kaypoh:
    command: "/absolute/path/to/kaypoh"
    args: ["mcp", "serve"]
    tools:
      include:
        - kaypoh_search_availability
        - kaypoh_list_venues
        - kaypoh_list_sources
        - kaypoh_list_watches
        - kaypoh_list_events
```

Restart `hermes chat` after editing the config. The current Hermes guide
documents `mcp_servers` with `command` and `args`, plus per-server tool filters:
[Hermes MCP documentation](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/mcp.md).

## OpenClaw

Save a local stdio definition, then probe it:

```sh
openclaw mcp add kaypoh \
  --command /absolute/path/to/kaypoh \
  --arg mcp \
  --arg serve \
  --include 'kaypoh_search_availability,kaypoh_list_venues,kaypoh_list_sources,kaypoh_list_watches,kaypoh_list_events'
openclaw mcp doctor kaypoh --probe
```

OpenClaw's current CLI supports stdio definitions with `--command` and repeated
`--arg`; `doctor --probe` proves the server connects and lists tools. See
[OpenClaw MCP CLI documentation](https://docs.openclaw.ai/cli/mcp).

Neither Hermes nor OpenClaw was installed in this workspace, so those two setup
commands are documented but not live-probed here.
