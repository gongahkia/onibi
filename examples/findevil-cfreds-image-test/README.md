# CFReDS Forensics Image Test Fixture

This fixture wraps the public CFReDS `Forensics Image Test image` case without
checking the image into git. The reproducible input is the fetch script plus the
pinned SHA-256 in that script.

## Fetch The Image

From the repository root:

```console
$ node scripts/fetch-cfreds-case.mjs
```

The script downloads `2020JimmyWilson.E01` to:

```text
.kelpclaw/datasets/cfreds/forensics-image-test/2020JimmyWilson.E01
```

It follows the Google Drive large-file confirmation flow, verifies the final
file size and SHA-256, and exits `0` only when the cached image matches:

```text
6c18f662744d55e2769d9510f6173f04dab668c42b67ef27b675d22e628b4ed5
```

The dataset cache is ignored by git; do not commit the E01.

## Expected Findings

The official companion PDF is a case-study worksheet for questions 1-25. It
contains prompts, not an answer key. `case.yml` therefore records the official
prompts as expected investigative findings: email metadata, VHD and partition
metadata, account records, recycle-bin activity, browser search history, logon
autoruns, file hashes, and the physical disk hash.

The PDF text repeats question 4 for a second disk-GUID prompt and skips question 21. The manifest keeps both question-4 prompts and preserves the source question
numbers.

## What Kelp Does With It

Kelp treats the fetched E01 as read-only evidence. A Sentinel run should hash
the cache before analysis, run the configured SIFT-style analysis command over
the mounted case directory, link resulting claims back to evidence artifacts,
and benchmark confirmed claims against the `expectedFindings` in `case.yml`.

Example shape:

```console
$ kelp-claw findevil sentinel \
  --case examples/findevil-cfreds-image-test/case.yml \
  --evidence-root .kelpclaw/datasets/cfreds/forensics-image-test \
  --sift-command "protocol-sift run --case-dir /mnt/case-ro --output-jsonl" \
  --max-iterations 3 \
  --out .kelpclaw/findevil/cfreds-image-test
```

The benchmark should not reward unsupported multiple-choice guesses. A claim
should be confirmed only when it cites the recovered artifact type named in the
manifest, such as email headers, partition metadata, SAM records, recycle-bin
metadata, browser history, autorun keys, or direct file hashes.
