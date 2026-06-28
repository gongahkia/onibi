# Web Intel

Status: retired from the Pi runtime.

The local-only Kelp Pi target has no live web-search provider surface, no web API keys, and no browser automation path. `packages/web-intel` remains legacy TypeScript reference code until final cutover.

Allowed replacement:

- Import pre-downloaded files as evidence.
- Index local HTML/text/PDF sidecars through `kelp-pi index ingest`.
- Cite local chunks from the offline retrieval index.

Not allowed in the Pi runtime:

- Exa/TinyFish provider calls.
- Browser agent tasks.
- Network research during an airgapped engagement.
- Any policy pack that grants live Internet access by default.
