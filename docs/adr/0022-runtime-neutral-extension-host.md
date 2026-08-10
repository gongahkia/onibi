# ADR 0022: keep generic extension invocation runtime-neutral and data-only

## Status

Accepted for M8.

## Context

M7 proved that Lua callbacks can return declarative semantic data, but direct
Lua callback handles in a general command or semantic registry would make Lua
the de facto extension ABI and leak adapter lifetime concerns into the runtime.

## Decision

`zenbu.model_api.Extension_host` represents an opaque invocation token,
provider, granted capabilities, and data-only `Extension_value` request and
response. Generic command and semantic behavior entries retain only that host
and token plus decoders. The Lua adapter privately maps its tokens to PUC Lua
callbacks. Context conversion copies only approved document/selection/syntax
data; results remain declarative effects, selections, or edit proposals.

## Consequences

Future runtimes can implement the same boundary without changing generic
registries or adding a privileged mutation path. The API is not a sandbox:
adapter code remains trusted until an isolated M9 runtime exists.
