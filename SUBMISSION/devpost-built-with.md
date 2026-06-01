# Built With

Public repository: `https://github.com/gongahkia/kelp-claw`

- TypeScript
- Node.js
- pnpm
- Claude Code
- Protocol SIFT
- SANS SIFT Workstation
- Tigma
- Volatility 3
- YARA
- MFTECmd
- Sigma
- MITRE ATT&CK
- MITRE ATT&CK Navigator format
- Sysmon
- Windows EVTX-style JSON
- ShimCache
- SRUM
- PCAP and Zeek-style flow summaries
- RFC3161 timestamp responses
- Docker
- GitHub Actions
- OpenAI SDK
- Google Generative AI SDK
- Anthropic SDK
- Mermaid
- Ed25519
- SHA-256
- JSONL
- Markdown
- Vitest

v3 adds Sigma-rule mapping and ATT&CK Navigator export to the reporting path, RFC3161 timestamp tokens to the audit bundle, Docker packaging through `Dockerfile.kelp`, and CI coverage through `.github/workflows/ci.yml`.

The v3 rerun anchors are synthetic precision 1.000 / recall 0.500 / F1 0.667, CFReDS precision 0.000 / recall 0.000 / F1 0.000, and DFIR-Metric blind subset-10 precision 0.000 / recall 0.000 / F1 0.000.
