# Phase 1 Local Lab Validation

Date: 2026-05-17

GitHub issue: #37

Status: partial validation pass. This run uses real nmap and nuclei exports from a local lab target, but it does not close #37 because Burp Suite Pro XML and design-partner review are still unavailable.

## Target And Inputs

- Target: OWASP Juice Shop running locally in Docker.
- Address scanned: `127.0.0.1:3001`.
- nmap input: real `nmap -sV -p 3001 -oX ... 127.0.0.1` XML.
- nuclei input: real `nuclei -jsonl` output from nuclei v3.8.0 with templates v10.4.3.
- Burp input: unavailable. Burp Suite Community Edition cannot export Issues XML, so #32 remains blocked.

Raw exports and generated workspaces are intentionally kept under `private-validation/` and are not committed because they include local machine details.

## Pipeline Run

Two workspaces were created:

- Baseline workspace: initialized with the nmap export only.
- Current workspace: initialized with nmap plus nuclei exports.

Commands exercised:

- `piranesi ingest init`
- `piranesi ingest nmap`
- `piranesi ingest nuclei`
- `piranesi report --format md`
- `piranesi report --format json`
- `piranesi report --format pdf --pdf-backend reportlab`
- `piranesi sign`
- `piranesi sign --verify`
- `piranesi retest`

## Results

- nmap ingest created 1 finding.
- nuclei ingest read 21 JSONL records and grouped them into 12 findings.
- Combined current workspace contained 13 findings.
- Severity summary: 12 info, 1 medium.
- Markdown report generated successfully.
- JSON report generated successfully.
- PDF report generated successfully with `reportlab`.
- Chain-of-custody manifest was created and verified successfully.
- Retest baseline-to-current summary: 1 open, 12 new, 0 changed, 0 closed, 0 regressed, 0 ambiguous.

## Observations

- Grouping worked for repeated nuclei `http-missing-security-headers` records: 10 source records became one report finding.
- Evidence redaction worked in the markdown report: nuclei request, response, and curl evidence rendered as `[redacted]`.
- The nmap service fingerprint labeled the local Juice Shop port as `nessus`; Piranesi preserved the tool output as reported. This is useful provenance, but a consultant-facing report may need clearer wording that service names are tool-observed and unverified.
- Two nuclei info detections looked like tool-side false positives for this target: `dameng-detect` and `snmpv3-detect`. Piranesi imported them correctly. This run identified the need to mark imported scanner assertions as tool-observed until reviewer confirmation.
- The default WeasyPrint PDF backend failed on this machine because WeasyPrint system libraries were unavailable. The `reportlab` backend generated a PDF successfully. This run identified the need for clearer backend setup and fallback guidance.

## Follow-Ups

- Keep #32 blocked until a real Burp Suite Pro Issues XML export is available.
- #37 still needs design-partner or qualified reviewer feedback on the generated report.
- #37 still needs a larger engagement-scale run, ideally approximating the 1,000-host goal when legally and practically available.
- Address the imported-finding confidence semantics follow-up so parser-provided findings are not presented as manual confirmation.
- Address the PDF backend setup follow-up and recommend `reportlab` as the reliable local fallback when WeasyPrint system dependencies are missing.
