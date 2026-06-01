# NIST CFReDS Hacking Case

This fixture is the real NIST CFReDS Hacking Case, not a synthetic trace. It
keeps the 1.09 GB EWF image out of git and records a smaller scored pilot set
of artifact-backed findings in `case.yml`.

## Fetch

```console
$ node scripts/fetch-cfreds-hacking-case.mjs
```

The script downloads:

- `4Dell Latitude CPi.E01` from the official CFReDS archive
- `4Dell Latitude CPi.E02` from the official CFReDS archive
- `TestAnswers.pdf`, pinned by SHA-256

The official answer PDF lists the acquisition image MD5 as:

```text
AEE4FCD9301C03B3B054623CA261959A
```

On the SIFT VM, verify the EWF image directly:

```console
$ cd .kelpclaw/datasets/cfreds/hacking-case
$ ewfverify "4Dell Latitude CPi.E01"
```

## SIFT Triage

Run this inside the SIFT VM after fetching the image:

```console
$ node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
```

The wrapper is read-only against the original image. It writes recovered
artifacts, command transcripts, and a Protocol-SIFT-style trace under:

```text
.kelpclaw/findevil/cfreds-hacking-case/triage/
```

Then run KelpClaw against the generated trace and artifact evidence:

```console
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --trace .kelpclaw/findevil/cfreds-hacking-case/triage/trace.jsonl \
  --max-iterations 3 \
  --timestamp skip \
  --out .kelpclaw/findevil/sentinel-cfreds-hacking-case
```

## Custom MCP Server

The read-only MCP server exposes typed forensic functions instead of generic
shell:

```console
$ ./node_modules/.bin/kelp-claw findevil mcp \
  --evidence-root .kelpclaw/datasets/cfreds/hacking-case \
  --max-runtime-seconds 180
```

Use `mcp-config.example.json` as the Claude Code MCP config template. The
server exposes only inventory, hashing, Sleuth Kit partition/file operations,
and literal artifact search. It does not expose arbitrary shell or write tools.

## Scope

The pilot set scores 8 findings from the 31 official worksheet questions. That
is intentional: the goal is to get the public CFReDS Hacking Case off the 0.000
anchor with recovered artifact proof while keeping unsupported worksheet
answers out of the confirmed ledger.
