# IDEA 2: KelpClaw SIFT Verifier

## Hackathon Fit

Find Evil! is focused on autonomous AI incident response with Protocol SIFT and the SANS SIFT Workstation. The hackathon asks builders to improve how Protocol SIFT processes case data across disk images, memory captures, endpoints, logs, pcaps, and other DFIR artifacts.

The strongest judging signal is not "security-themed software." It is an autonomous workflow that can analyze real case data, preserve evidence integrity, self-correct when it overclaims, and leave behind a complete execution trail.

Source: https://findevil.devpost.com/

KelpClaw already has several relevant primitives:

- Agent execution traces
- Policy decisions and approval records
- Replayable run manifests
- Evidence workspaces and signed audit bundles
- Skill wrappers for Codex, Claude Code, and Goose
- Cross-agent replay comparison
- Static reviewer bundles

IDEA 1 turns KelpClaw into a typed SIFT tool bridge and a self-correcting disk triage agent.

IDEA 2 should pivot differently: make KelpClaw the accuracy, evidence, and repair harness around Protocol SIFT.

## Project Framing

**KelpClaw SIFT Verifier** is an autonomous claim-verification and repair layer for Protocol SIFT.

Instead of building the primary forensic tool bridge, KelpClaw SIFT Verifier wraps an existing Protocol SIFT run, captures its execution trace, decomposes the final report into atomic forensic claims, checks every claim against evidence, and launches bounded repair passes when a claim is unsupported or contradicted.

Suggested one-liner:

> KelpClaw SIFT Verifier turns Protocol SIFT reports into evidence-backed claim ledgers, forcing autonomous repair when findings are unsupported, contradicted, or missing proof.

## How This Is Different From IDEA 1

IDEA 1 is centered on:

- A custom SIFT MCP server
- Typed forensic tools
- Read-only tool execution boundaries
- A specific disk timeline triage workflow
- Direct control over SIFT analysis steps

IDEA 2 is centered on:

- Protocol SIFT output verification
- Claim-to-evidence mapping
- Accuracy scoring
- Hallucination detection
- Bounded repair prompts
- Benchmarking baseline versus repaired runs

The core artifact is not a new SIFT tool adapter. The core artifact is a verifier that can say:

- What did Protocol SIFT claim?
- What evidence supports each claim?
- Which claims are only inferred?
- Which claims are unsupported?
- Which claims contradict the case data?
- What did the agent rerun to repair the report?
- Did the final report improve?

## Product Concept

Protocol SIFT produces an investigation report. KelpClaw SIFT Verifier turns that report into an audit-grade claim ledger.

Example claim:

```json
{
  "id": "claim-0007",
  "text": "evil.exe executed from C:\\Users\\Public\\Downloads",
  "type": "program_execution",
  "severity": "high",
  "status": "unsupported",
  "confidence": 0.38,
  "evidenceRefs": [
    {
      "artifact": "timeline.csv",
      "locator": "row:1842",
      "supports": "file_present",
      "hash": "sha256:..."
    }
  ],
  "missingEvidence": [
    "prefetch_entry",
    "amcache_execution_record",
    "sysmon_process_create"
  ],
  "repairAction": {
    "kind": "targeted_reanalysis",
    "prompt": "Prove, retract, or downgrade the claim that evil.exe executed. Check Prefetch, Amcache, ShimCache, Sysmon, and timeline evidence."
  }
}
```

The verifier does not need to prove every possible forensic fact. It needs to prove that the agent knows the difference between:

- A file existing
- A file executing
- A file persisting
- A process connecting to the network
- A user action
- A timeline correlation
- A confirmed incident conclusion

That distinction is exactly where autonomous IR agents can fail.

## Architecture

### 1. Protocol SIFT Runner

Runs an existing Protocol SIFT workflow against case data.

The runner captures:

- Prompt or task
- Case manifest
- Tool calls
- Tool outputs
- Model metadata when available
- Token usage when available
- Generated artifacts
- Final report

This can be implemented using the existing KelpClaw skill runner and wrapper path instead of creating a new SIFT MCP bridge.

### 2. Trace Normalizer

Converts Protocol SIFT logs, shell output, MCP events, and generated files into a stable KelpClaw trace format.

Output:

- `agent-execution.jsonl`
- `tool-events.jsonl`
- `artifact-manifest.json`
- `run-metadata.json`

### 3. Claim Extractor

Reads the final Protocol SIFT report and extracts atomic claims.

Claim categories:

- `file_presence`
- `program_execution`
- `persistence`
- `privilege_escalation`
- `credential_access`
- `network_connection`
- `lateral_movement`
- `data_exfiltration`
- `user_activity`
- `timeline_ordering`
- `malware_identification`
- `incident_conclusion`

Each claim should have:

- Claim text
- Claim type
- Severity
- Confidence
- Referenced artifacts
- Required evidence types
- Source paragraph or line

### 4. Evidence Linker

Maps each claim to actual evidence from the case output.

Evidence can come from:

- Timeline CSV rows
- Prefetch output
- Amcache output
- ShimCache output
- Sysmon logs
- Windows Event Logs
- Process listings
- Memory analysis outputs
- PCAP flow summaries
- Hash scan results
- YARA matches
- File metadata
- Protocol SIFT tool output

The first MVP does not need every adapter. It needs enough to demonstrate the verifier loop on one case.

### 5. Autonomous Verifier

Evaluates each claim against linked evidence.

Statuses:

- `confirmed`: direct evidence supports the claim
- `inferred`: evidence suggests the claim but does not prove it
- `unsupported`: the claim lacks required evidence
- `contradicted`: available evidence conflicts with the claim
- `unverifiable`: evidence is missing or outside available data

The verifier should be stricter for high-impact claims. For example, "execution" should not be confirmed from file presence alone.

### 6. Repair Loop

When a claim is unsupported or contradicted, the verifier creates a targeted repair task for Protocol SIFT.

Loop:

1. Run Protocol SIFT and capture report.
2. Extract claims.
3. Link evidence.
4. Mark unsupported, contradicted, or unverifiable claims.
5. Generate targeted repair tasks.
6. Run a bounded repair pass.
7. Re-score the repaired report.
8. Stop when critical claims are confirmed/retracted or `--max-iterations` is reached.

The repair loop should write `repair-trace.jsonl` so judges can see exactly how the system self-corrected.

### 7. Accuracy Scorer

Compares baseline and repaired runs.

Metrics:

- Confirmed findings
- False positives
- Unsupported claims
- Contradicted claims
- Rejected or retracted claims
- Missed known findings when ground truth exists
- Evidence coverage per finding
- Repair success rate
- Iterations to convergence

This can be positioned as an accuracy benchmarking framework plus autonomous repair layer.

## Focused Demo Workflow

Recommended demo:

**Claim Repair for Program Execution Evidence**

Input: one prepared case dataset and one Protocol SIFT run.

Demo flow:

1. Protocol SIFT analyzes the case and writes an incident report.
2. The report claims that a suspicious executable ran.
3. KelpClaw SIFT Verifier extracts that as a `program_execution` claim.
4. The evidence linker finds file presence in the timeline but no execution proof.
5. The verifier marks the claim `unsupported`.
6. A repair task asks Protocol SIFT to prove, retract, or downgrade the execution claim.
7. Protocol SIFT reruns targeted analysis against Prefetch, Amcache, ShimCache, Sysmon, or available equivalent artifacts.
8. The repaired report either confirms the execution claim with evidence or downgrades it to file presence.
9. KelpClaw exports the claim ledger, accuracy report, repair trace, and audit bundle.

This gives the demo a visible before/after:

- Baseline report: overclaims execution.
- Verifier: catches the overclaim.
- Repair pass: corrects the conclusion.
- Final report: evidence-backed and auditable.

## Implementation Targets In This Repo

Likely locations:

- `examples/findevil-sift-verifier/`
- `packages/evidence/src/`
- `packages/cli/src/`
- `packages/testing/src/`
- `.kelpclaw/runs/<runId>/`

Suggested CLI:

```console
$ kelp-claw findevil verify \
  --case examples/findevil-sift-verifier/case.yml \
  --sift-run .kelpclaw/runs/protocol-sift-baseline \
  --max-iterations 3 \
  --out .kelpclaw/findevil/sift-verifier
```

Expected output:

```text
.kelpclaw/findevil/sift-verifier/
  agent-execution.jsonl
  artifact-manifest.json
  baseline-report.md
  repaired-report.md
  claim-ledger.json
  evidence-manifest.json
  repair-trace.jsonl
  accuracy-report.md
  audit-bundle/
    index.html
    manifest.json
    attestation.json
```

## Case Manifest

Add a simple `case.yml`:

```yaml
id: findevil-demo-case-001
name: Suspicious Download Execution
evidenceRoot: ./case-data
readonly: true
expectedFindings:
  - id: finding-001
    type: program_execution
    description: Suspicious executable execution should be confirmed only if execution artifacts exist.
    acceptedEvidence:
      - prefetch_entry
      - amcache_execution_record
      - sysmon_process_create
      - shimcache_execution_indicator
scoring:
  requireEvidenceForHighSeverity: true
  allowInferenceLabels: true
```

The manifest gives the scorer enough structure to judge Protocol SIFT without hardcoding one dataset forever.

## Submission Artifacts

The Devpost submission should include:

- Public GitHub repo with MIT or Apache 2.0 license
- <=5-minute demo video showing:
  - baseline Protocol SIFT report
  - unsupported or contradicted claim detection
  - targeted repair pass
  - repaired final report
  - exported audit bundle
- Architecture diagram showing:
  - Case data
  - Protocol SIFT runner
  - KelpClaw trace capture
  - Claim extractor
  - Evidence linker
  - Verifier agent
  - Repair loop
  - Accuracy scorer
  - Audit bundle
- Written project description
- Dataset documentation
- Accuracy report
- Try-it-out instructions for SIFT Workstation
- Structured execution logs with timestamps, model/tool metadata, and token usage when available

## How To Position It

Lead with accuracy and self-correction.

Good positioning:

> KelpClaw SIFT Verifier makes autonomous DFIR reports defensible: every Protocol SIFT finding is checked against evidence, unsupported claims trigger repair, and reviewers receive a signed claim ledger showing what was confirmed, inferred, rejected, or unresolved.

Avoid positioning it as:

- A generic governance dashboard
- A compliance report generator
- A replacement for SIFT
- A full forensic tool wrapper
- Another MCP gateway

The hackathon-facing story is: Protocol SIFT can move fast; KelpClaw makes it prove its findings.

## Fastest Viable Implementation

Minimum viable version:

1. Create one example case manifest.
2. Support ingesting one Protocol SIFT report and one trace/artifact directory.
3. Implement a deterministic claim schema and simple report claim extractor.
4. Implement evidence linking for 2-3 artifact types.
5. Implement strict verification rules for one claim type, preferably `program_execution`.
6. Generate one targeted repair prompt.
7. Compare baseline and repaired reports.
8. Export:
   - `claim-ledger.json`
   - `accuracy-report.md`
   - `repair-trace.jsonl`
   - `evidence-manifest.json`
   - `agent-execution.jsonl`
   - `audit-bundle/index.html`
9. Record the demo showing one concrete self-correction.

## Why This Can Win

This idea targets the hardest part of autonomous incident response: not producing a report, but knowing whether the report is true.

It also fits KelpClaw better than a large SIFT integration because KelpClaw is already built around proof, replay, evidence, policy, and reviewer handoff. The project can show immediate hackathon value without wrapping the entire SIFT Workstation.

The strongest demo is a visible correction:

1. Protocol SIFT makes an overclaim.
2. KelpClaw catches it.
3. A bounded repair loop forces the agent to prove or retract it.
4. The final report is more accurate.
5. The audit bundle proves exactly how the correction happened.

