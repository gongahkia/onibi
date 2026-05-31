# Challenges

The main challenge was keeping the project honest. A polished demo could hide the failure mode we wanted to show, so v3 uses three anchors: a synthetic case that exercises self-correction, a CFReDS public image that stays conservative without live artifact parsing, and DFIR-Metric subset-10 for a repeatable benchmark path.

A second challenge was separating evidence from instructions. DFIR data often contains filenames, ransom notes, scripts, log messages, and user documents. The firewall had to treat those strings as evidence while preventing them from crossing into tool arguments. The Phase 12B corpus now gives a measurable answer: 46 of 46 malicious payloads blocked, 9 of 9 quote controls allowed.

A third challenge was timestamping and reproducibility. RFC3161 TSA tokens depend on an external timestamp authority, while deterministic replay must avoid fresh model calls. v3 handles both: audit bundles include `evidence-manifest.tsr`, and deterministic replay asserts the stable claim-ledger hash `sha256:8f99da2da7cb45a9e28d0c6db0c89fe6d08cbcf36fa0d2a710cd9552a10ee666`.

The remaining implementation gap is CFReDS live artifact parsing in the container anchor. The current run verifies the pinned E01 and emits 25 worksheet claims, but does not confirm any prompt without recovered email, VHD, SAM, recycle-bin, browser, autorun, or file-hash evidence.
