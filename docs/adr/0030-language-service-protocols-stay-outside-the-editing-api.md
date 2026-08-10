# ADR 0030: language-service protocols stay outside the editing API

## Status

Accepted for M11.

## Context

Language servers use JSON-RPC processes, URI strings, negotiated encodings,
and generated protocol types. Editing models need diagnostics, locations, and
text edits, but making transport detail public would couple future models to
LSP and force other intelligence sources to imitate it.

## Decision

`zenbu.language` owns data values for server configuration, diagnostics,
hover, definitions, completion, text edits, position conversion, and document
sync. `zenbu.lsp` privately depends on maintained `lsp` and `jsonrpc` packages
and projects protocol values into that data. Neither `zenbu.kernel` nor
`zenbu.model_api` depends on `zenbu.lsp`, and their public interfaces expose no
LSP/JSON-RPC values. Session owns process lifecycle and applies returned edits
through an existing semantic transformation.

## Consequences

Every editing model receives the same host language commands. A future non-LSP
provider can target `zenbu.language` values or another host adapter. The
private adapter may change transport implementation without breaking models.
