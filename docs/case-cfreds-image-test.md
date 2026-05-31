# CFReDS Forensics Image Test Case

## Provenance

- Portal: https://cfreds.nist.gov/all/DFIR_AB/ForensicsImageTestimage
- Archive landing page: https://cfreds-archive.nist.gov/
- Image file: `2020JimmyWilson.E01`
- Image URL: https://drive.google.com/file/d/1Fd1pX1r4waRkD6Z2O8J5cRZyeSNU5-SY/view?usp=sharing
- Companion PDF: `Case Study_Forensic image Test.pdf`
- Companion PDF URL: https://drive.google.com/file/d/1TpXtoSUQ-ZgfVXZ0PfxRfQyTTWYu1Dw-/view?usp=sharing
- Author listed by CFReDS: `DFIR_AB`
- Inputter listed by CFReDS: `Ujunwa Okonkwo`

## License / Use Terms

The CFReDS portal entry and archive landing page do not publish an explicit
license for this contributed case. Treat the fixture as public CFReDS
training/tool-validation material with attribution to CFReDS and `DFIR_AB`, and
do not redistribute the downloaded E01 from this repository.

## Pinned Image Integrity

The portal entry points to a Google Drive-hosted E01. The portal does not expose
a SHA-256 field, so `scripts/fetch-cfreds-case.mjs` pins the byte stream fetched
from the official Drive URL.

- Expected SHA-256: `6c18f662744d55e2769d9510f6173f04dab668c42b67ef27b675d22e628b4ed5`
- Observed SHA-1: `711b07ad5b8f7be3a7f4bdf84f274e02e57d2db5`
- Expected file size: `309818835` bytes
- File type: EWF / Expert Witness / EnCase image
- Cache path: `.kelpclaw/datasets/cfreds/forensics-image-test/2020JimmyWilson.E01`

The companion PDF is `146143` bytes with SHA-256
`90ffae48ec0d933e69a9fb7b94baecf06de04c214d6f59ab93bd166d79c12573`.

## Why This Works For A 5-Minute Demo

The case is a single 295 MiB E01 plus a small official worksheet, so the fetch
and hash check are quick and reproducible. The worksheet gives concrete,
auditable prompts for email headers, partitions, account records, recycle-bin
activity, browser history, autoruns, file hashes, and disk hashing. Kelp can
show the useful demo path without shipping evidence in git: fetch, verify,
hash before analysis, run a SIFT-style analyzer, link claims to artifacts, and
reject unsupported multiple-choice guesses.
