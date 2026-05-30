# Protocol SIFT Baseline Report

## Executive Summary

Protocol SIFT reviewed `WIN-LAB01` after a suspicious download incident. The
baseline report intentionally mixes confirmed evidence, weak indicators, and
overclaimed conclusions so KelpClaw SIFT Sentinel can exercise every verifier
rule family and repair path.

## Findings

### F-001 PowerShell Program Execution

- Claim: `powershell.exe` executed from
  `C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe`.
- Severity: High.
- Evidence cited: `timeline.csv row:1848` shows a PowerShell process event, and
  the analyst notes that a Prefetch check should confirm execution.
- Analyst conclusion: PowerShell executed during the incident window.

### F-002 Invoice Viewer Program Execution

- Claim: `invoice_viewer.exe` executed from
  `C:/Users/Public/Downloads/invoice_viewer.exe`.
- Severity: High.
- Evidence cited: `timeline.csv row:1849` shows only file presence in Public
  Downloads.
- Analyst conclusion: The executable ran after download.

### F-003 Ghost Tool Program Execution

- Claim: `ghosttool.exe` executed from
  `C:/Users/Public/Downloads/ghosttool.exe`.
- Severity: High.
- Evidence cited: `amcache/Amcache-evidence.json record:ghosttool-deny` states
  that Amcache has no installed inventory record for `ghosttool.exe`.
- Analyst conclusion: The executable still executed despite the negative
  inventory note.

### F-004 Run-Key Persistence

- Claim: The `UpdaterRun` Run-key value persisted
  `C:/ProgramData/Updater/updater.exe`.
- Severity: High.
- Evidence cited: `timeline.csv row:1851` and `registry/run-keys.json` identify
  an `HKCU/.../Run` value for `updater.exe`.
- Analyst conclusion: The Run-key persistence mechanism is confirmed.

### F-005 DailyUpdater Task Persistence

- Claim: The `DailyUpdater` scheduled task persisted
  `C:/ProgramData/Updater/taskdrop.exe`.
- Severity: High.
- Evidence cited: `timeline.csv row:1852` and
  `registry/scheduled-tasks.json` mention a TaskCache reference but do not show a
  TaskScheduler creation event.
- Analyst conclusion: Scheduled-task persistence is established.

### F-006 C2 Network Connection

- Claim: `powershell.exe` connected to `c2.example.test` over TCP/443.
- Severity: High.
- Evidence cited: `timeline.csv row:1853` and `pcap/flow-summary.json` summarize
  an outbound TLS flow from `WIN-LAB01`.
- Analyst conclusion: The outbound C2 connection is confirmed.

### F-007 DNS-Only Network Connection

- Claim: `WIN-LAB01` established a network connection to
  `cdn-update.example.test`.
- Severity: High.
- Evidence cited: `timeline.csv row:1854` shows a DNS lookup for the host but no
  matching flow record.
- Analyst conclusion: The DNS lookup proves an established connection.

### F-008 Credential Access

- Claim: `powershell.exe` attempted credential access by invoking
  `comsvcs.dll` MiniDump against `lsass.exe`.
- Severity: High.
- Evidence cited: `timeline.csv row:1855` records the suspicious command-line
  indicator.
- Analyst conclusion: Credential access is likely.

### F-009 Lateral Movement

- Claim: The attacker laterally moved from `WIN-LAB01` to `WIN-FILE01` using a
  remote logon.
- Severity: High.
- Evidence cited: No remote-logon event, SMB session log, or destination-host
  telemetry is supplied.
- Analyst conclusion: Lateral movement occurred.

### F-010 Malware Identification

- Claim: `invoice_viewer.exe` is the `EvilClaw` malware family.
- Severity: Medium.
- Evidence cited: `timeline.csv row:1856` records a YARA-style
  `EvilClaw_Packed` rule hit, but the report does not provide a full hash chain.
- Analyst conclusion: The malware family identification is likely.

## Notes

This report is intentionally overclaimed for the KelpClaw SIFT Sentinel demo.
File presence, DNS lookups, TaskCache references, missing remote-logon evidence,
and YARA hits without a hash chain are not sufficient for every conclusion above.
