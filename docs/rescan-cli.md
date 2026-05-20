# Rescan CLI

`piranesi rescan --from-baseline <workspace>` is an explicit replay command for
scanner evidence that was already imported into a baseline workspace. It does not run
during `ingest`, `report`, `retest`, `sign`, or `serve`.

Inspect recoverable replay specs without Docker:

```bash
piranesi rescan --from-baseline ./workspace-before --dry-run --json
```

Execute replay into a current workspace:

```bash
piranesi rescan \
  --from-baseline ./workspace-before \
  --output-workspace ./workspace-after \
  --image nmap=ghcr.io/example/nmap:v1@sha256:<digest> \
  --image nuclei=ghcr.io/example/nuclei:v1@sha256:<digest> \
  --allow-unenforced-network
```

The command uses replay extractor output only. Supported tools are currently nmap
XML and nuclei JSONL baselines. Unsupported evidence is skipped by extraction, and a
workspace with no supported baseline evidence fails closed for execution.

Execution requirements:

- optional rescan runtime support is installed;
- Docker CLI and daemon are available;
- every recovered tool has a `--image tool=repo:tag@sha256:<digest>` override;
- images are already available locally, because Piranesi does not pull implicitly;
- Piranesi derives the intended network scope from baseline evidence and rejects
  recovered commands that would expand beyond it;
- `--allow-unenforced-network` is supplied when Docker egress allowlisting cannot be
  enforced by the local runtime. The override is recorded in rescan provenance.

Successful replay writes raw files under `raw/<tool>/` in the output workspace with
rescan provenance metadata. Those files remain in the same nmap XML or nuclei JSONL
shape that the existing `piranesi ingest <tool> --input ...` commands already
consume.
