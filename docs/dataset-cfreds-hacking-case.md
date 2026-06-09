# Dataset Documentation: NIST CFReDS Hacking Case

## Source

KelpClaw's real-image pilot uses the public NIST CFReDS Hacking Case.

- Archive page: `https://cfreds-archive.nist.gov/Hacking_Case.html`
- Answer PDF: `https://cfreds-archive.nist.gov/images/TestAnswers.pdf`
- Image part 1: `https://cfreds-archive.nist.gov/images/4Dell%20Latitude%20CPi.E01`
- Image part 2: `https://cfreds-archive.nist.gov/images/4Dell%20Latitude%20CPi.E02`

The repository does not commit the disk image. The acquisition is downloaded by
`scripts/fetch-cfreds-hacking-case.mjs` into:

```text
.kelpclaw/datasets/cfreds/hacking-case/
```

The script verifies file sizes and pins `TestAnswers.pdf` by SHA-256. The
official answer PDF records this acquisition MD5:

```text
AEE4FCD9301C03B3B054623CA261959A
```

On SIFT Workstation, verify the image with:

```console
$ cd .kelpclaw/datasets/cfreds/hacking-case
$ ewfverify "4Dell Latitude CPi.E01"
```

## How The Evidence Is Prepared

The SIFT triage wrapper is:

```text
scripts/run-cfreds-hacking-case-triage.mjs
```

It runs read-only forensic collection against the original EWF image:

1. Write an original-image manifest for the E01/E02 parts.
2. Run `ewfverify` and `ewfinfo`.
3. Mount the EWF image with `ewfmount`.
4. Use the exposed raw view, normally `ewf1`.
5. Run `mmls` to identify the filesystem offset.
6. Run `fsstat` for filesystem metadata.
7. Run `tsk_recover` to recover files into the KelpClaw evidence root.
8. Run RegRipper plugins where registry hives are recovered.
9. Write recovered-file inventory and literal indicator search artifacts.

The wrapper writes generated evidence under:

```text
.kelpclaw/findevil/cfreds-hacking-case/triage/evidence/
```

The original image remains outside git and is not modified by KelpClaw.

## Scored Pilot Scope

The public worksheet has 31 questions. This pilot scores 8 artifact-backed
findings from `examples/findevil-cfreds-hacking-case/case.yml`:

| Finding                              | Evidence requirement                          |
| ------------------------------------ | --------------------------------------------- |
| EWF acquisition MD5                  | `ewfverify_hash_match`, `image_hash_manifest` |
| Windows XP OS metadata               | `registry_os_metadata`                        |
| Greg Schardt registered owner        | `registry_registered_owner`                   |
| N-1A9ODN6ZXK4LQ / Evil host identity | `registry_computer_identity`                  |
| Mr. Evil user account                | `user_account_artifact`                       |
| Look@LAN identity config             | `lookatlan_identity_config`                   |
| Hacking or dual-use tools            | `hacking_tool_indicator_search`               |
| Interception traffic artifact        | `captured_traffic_file`                       |

The scope is intentionally conservative. KelpClaw does not promote worksheet
answers to confirmed findings unless recovered artifacts support them.

## Reproducibility

Inside the SIFT VM:

```console
$ node scripts/fetch-cfreds-hacking-case.mjs
$ node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
$ node packages/cli/dist/index.js findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --sift-command "node scripts/run-cfreds-hacking-case-triage.mjs --dataset .kelpclaw/datasets/cfreds/hacking-case --out .kelpclaw/findevil/cfreds-hacking-case/triage --mode emit-trace" \
  --max-iterations 3 \
  --repair-runner evidence-linked \
  --timestamp skip \
  --deterministic \
  --out .kelpclaw/findevil/cfreds-hacking-case/sentinel
```

The deterministic run is designed for judging and report generation. It uses
the evidence-linked repair runner and `--timestamp skip`, so it does not depend
on live model calls or an RFC3161 timestamp authority.

## Submission Results To Fill After Live Run

Copy these values from the final Sentinel output before submitting:

- Run directory:
- Audit bundle path:
- Confirmed claims:
- Unsupported or contradicted claims after repair:
- Precision / recall / F1:
- Hallucination count and hallucination rate:
- Spoliation check result:
- Number of firewall events:
- Traceability proof: one claim ID plus its producing `toolUseId`:
