# Protocol SIFT Baseline Report

## Executive Summary

The host `WIN-LAB01` contains a suspicious executable at
`C:/Users/Public/Downloads/evil.exe`. Based on the timeline entry at `row:1842`,
Protocol SIFT concludes that `evil.exe` executed and assigns high severity.

## Findings

### F-001 Suspicious Program Execution

- Claim: `evil.exe` executed from `C:/Users/Public/Downloads/evil.exe`.
- Severity: High.
- Evidence cited: `timeline.csv row:1842` shows the suspicious executable was
  present in Public Downloads.
- Analyst conclusion: The executable ran during the incident window.

## Notes

This report is intentionally overclaimed for the KelpClaw SIFT Sentinel demo.
File presence alone is not sufficient execution evidence.
