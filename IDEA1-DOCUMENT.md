# IDEA 1: KelpClaw SIFT

## Hackathon Fit

Find Evil! asks builders to make Protocol SIFT and the SANS SIFT Workstation operate as a stronger autonomous incident response agent. The target is not a generic security dashboard. The target is an agent that can process real case data, sequence forensic analysis, catch contradictions, self-correct, preserve evidence integrity, and produce traceable execution logs.

KelpClaw already has several pieces the hackathon judges care about:

- Structured agent execution logs
- Policy gates and approval/audit records
- Evidence hashing and signed bundles
- MCP-oriented tool boundaries
- Replay and traceability primitives
- Iteration and budget controls

The pivot is to focus those capabilities on digital forensics and incident response instead of general workflow governance.

## Project Framing

**KelpClaw SIFT** is an evidence-preserving, self-correcting incident response agent runtime for Protocol SIFT.

Instead of giving an LLM raw shell access to a forensic workstation, KelpClaw SIFT exposes typed forensic tools through an MCP bridge. The agent can run SIFT analysis steps, but only through safe, structured functions. It logs each step, validates each claim against tool output, reruns targeted analysis when evidence is missing or contradictory, and exports an audit bundle that links every finding to the exact execution that produced it.

Suggested one-liner:

> KelpClaw SIFT turns Protocol SIFT into a governed autonomous DFIR agent: typed forensic tools, read-only evidence handling, self-correcting analysis loops, and complete traceability from finding to tool execution.

## Build Plan

### 1. SIFT MCP Bridge

Add a purpose-built MCP server, likely under `apps/sift-mcp-server` or `packages/sift-mcp`, that exposes typed forensic functions instead of arbitrary shell.

Candidate tools:

- `list_partitions(image_path)`
- `extract_mft_timeline(image_path)`
- `analyze_prefetch(image_path)`
- `extract_amcache(image_path)`
- `run_yara_scan(target_path, ruleset)`
- `analyze_memory_pslist(memory_path)`
- `analyze_network_pcap(pcap_path)`

The implementation can call SIFT tools internally, but the agent should never receive a generic `execute_shell_cmd` capability for the demo path.

### 2. Read-Only Evidence Protection

The architecture should enforce evidence integrity at the filesystem/tool layer, not only through prompts.

Required behavior:

- Mount or open original case data read-only.
- Hash input evidence before analysis.
- Hash evidence again after analysis.
- Write derived artifacts to a separate workspace.
- Emit an explicit spoliation check in the final report.
- Keep raw evidence separate from generated reports and parsed artifacts.

This is important because Find Evil judges explicitly care whether guardrails are architectural or prompt-based.

### 3. Self-Correcting IR Loop

Implement a narrow autonomous loop:

1. Plan the next forensic step.
2. Run one typed SIFT/MCP tool.
3. Parse and summarize the tool result.
4. Validate new claims against observed evidence.
5. Identify unsupported claims, contradictions, or missing artifacts.
6. Rerun targeted analysis with adjusted parameters.
7. Stop when success criteria are met or `--max-iterations` is reached.

Every iteration should write structured JSONL with timestamps, selected tool, inputs, outputs, model/token metadata when available, validation result, and reason for the next step.

### 4. Focused Demo Workflow

Do not try to wrap all SIFT tools. The strongest fast path is one polished workflow.

Recommended demo:

**Self-Correcting Disk Timeline Triage**

Input: one disk image or prepared case data directory.

Agent flow:

- Identify partitions and mounted filesystem structure.
- Extract filesystem timeline.
- Search for suspicious executables, scripts, persistence locations, or recently modified artifacts.
- Parse Prefetch and/or Amcache where available.
- Cross-check timeline claims against Prefetch/Amcache evidence.
- Run targeted hash/YARA checks.
- Produce confirmed findings, rejected hypotheses, unresolved questions, and evidence links.

Demo self-correction example:

- First pass claims a suspicious executable ran.
- Validator sees only filesystem presence, not execution evidence.
- Agent reruns Prefetch/Amcache extraction.
- Final report downgrades or confirms the claim based on returned artifacts.

## Submission Artifacts

The Devpost submission should include:

- Public GitHub repo with MIT or Apache 2.0 license.
- Five-minute demo video showing live terminal execution and at least one self-correction.
- Architecture diagram showing:
  - Agent
  - KelpClaw runtime
  - SIFT MCP bridge
  - SIFT tools
  - Read-only case data
  - Derived artifact workspace
  - Audit/report output
  - Security boundaries
- Written project description:
  - What it does
  - How it was built
  - Challenges
  - What was learned
  - What comes next
- Dataset documentation:
  - What case data was used
  - Where it came from
  - What the agent found
- Accuracy report:
  - Confirmed findings
  - False positives
  - Missed artifacts
  - Hallucinated or rejected claims
  - Evidence integrity approach
  - Spoliation test result
- Try-it-out instructions for SIFT Workstation.
- Agent execution logs showing the full tool execution sequence.

## How To Position It

Lead with autonomous incident response, not with generic workflow governance.

Good positioning:

> KelpClaw SIFT makes Protocol SIFT safer and more autonomous by replacing raw shell access with typed forensic tools, enforcing read-only evidence boundaries, and preserving a full execution trace for every finding.

Avoid positioning it as:

- A general workflow builder
- A compliance dashboard
- A policy pack framework
- A generic MCP gateway

Those are supporting capabilities. The hackathon-facing story is: autonomous DFIR with evidence integrity and self-correction.

## Fastest Viable Implementation

Minimum viable version:

1. Add SIFT MCP server with 3-5 typed forensic tools.
2. Add a `sift-triage` CLI command or workflow template.
3. Add read-only input and derived-output workspace handling.
4. Add a self-correction loop with `--max-iterations`.
5. Produce:
   - `agent-execution.jsonl`
   - `findings.json`
   - `accuracy-report.md`
   - `evidence-manifest.json`
   - `audit-bundle/index.html`
6. Record a demo on one case dataset showing a correction.

## Why This Can Win

The strongest part of this repo is already aligned with the judging criteria: traceability, constraints, evidence handling, and agent governance. The missing piece is SIFT-specific forensic action.

If the pivot adds a real SIFT tool bridge and a narrow but convincing self-correcting triage loop, KelpClaw can credibly compete as a secure architecture for autonomous incident response rather than just another prompt wrapper.
