# Rescan Runtime Support

`rescan` is optional. The default Piranesi install does not include Docker Python
dependencies and does not require Docker to run `ingest`, `evidence`, `report`,
`retest`, `sign`, or `serve`.

Install optional rescan support with one of:

```bash
pip install "piranesi[rescan]"
uv sync --extra rescan
```

Runtime detection checks both the optional Docker Python package and the Docker CLI.
If either is missing, rescan code returns an actionable error instead of failing with
an import traceback.

This only enables the local runtime boundary. Replay still requires supported
extractors, digest-pinned images, recovered baseline scope, and the fail-closed
policy documented in [the rescan RFC](rfcs/rescan-execution-layer.md).
