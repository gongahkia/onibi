# SIFT Workstation Live Sentinel Setup

This is the live Protocol SIFT path for judges who have a SANS SIFT Workstation VM. The upstream Protocol SIFT README does not define a `protocol-sift` binary. Its invocation contract is: install the Claude Code global settings and skills into `~/.claude/`, prepare a case directory under `/cases/<CASE>`, mount evidence read-only, then launch `claude` from the case root. The `protocol-sift run ... --output-jsonl` command used by KelpClaw is a thin local wrapper around that contract.

Sources checked on 2026-05-30:

- SANS SIFT Workstation download page: https://www.sans.org/tools/sift-workstation/
- Protocol SIFT README: https://github.com/teamdfir/protocol-sift
- Protocol SIFT installer: https://raw.githubusercontent.com/teamdfir/protocol-sift/main/install.sh

## 1. Install SIFT Workstation 2026.1

1. Download the SIFT Workstation OVA from the SANS page:
   `https://www.sans.org/tools/sift-workstation/`
2. Use the login-gated "Download SIFT Workstation (OVA, 8.74GB)" link.
3. Verify the OVA before import:
   ```bash
   sha256sum SIFT-Workstation.ova
   ```
4. Expected SHA256 for the 2026-03-26 SANS OVA:
   ```text
   cd2edb7d707f1ff71db02ca3aca1967ecc76e34d378a522f184c5b70de311a09
   ```
5. Import the VM into VMware Fusion on the Mac: `File` -> `Import...` -> select the SIFT OVA -> choose a local VM folder.
6. Before first boot, allocate at least 4 CPU cores, 12-16 GB RAM, and 80 GB disk growth space. The Hacking Case E01/E02 plus recovered files need room.
7. Boot it and log in.
8. Default login:
   ```text
   username: sansforensics
   password: forensics
   ```

## 2. Install Protocol SIFT Inside the VM

Install Claude Code, clone Protocol SIFT, then run its installer:

```bash
sudo apt-get update
sudo apt-get install -y git nodejs npm python3-pip
sudo npm install -g @anthropic-ai/claude-code
git clone --depth=1 https://github.com/teamdfir/protocol-sift.git ~/protocol-sift
cd ~/protocol-sift
bash install.sh
pip3 install --user weasyprint
```

The installer copies global Claude Code configuration, DFIR skills, case templates, and report tooling into `~/.claude/`. Keep the clone if you want to update later with `git pull && bash install.sh`.

## 3. Set `ANTHROPIC_API_KEY`

Set the key without writing the secret into shell history:

```bash
read -rsp "ANTHROPIC_API_KEY: " ANTHROPIC_API_KEY
printf "\n"
printf "ANTHROPIC_API_KEY=%s\n" "$ANTHROPIC_API_KEY" | sudo tee -a /etc/environment >/dev/null
unset ANTHROPIC_API_KEY
```

Log out and back in, or load it for the current shell:

```bash
set -a
. /etc/environment
set +a
claude --version
```

## 4. Mount the Case Directory Read-Only

Copy or share the repository into the VM, then mount only the evidence directory read-only:

```bash
cd ~/kelp-claw
sudo mkdir -p /mnt/case-source /mnt/case-ro
sudo mount --bind "$PWD/examples/findevil-sift-sentinel/case-data" /mnt/case-source
sudo mount --bind /mnt/case-source /mnt/case-ro
sudo mount -o remount,bind,ro /mnt/case-ro
findmnt -no TARGET,OPTIONS /mnt/case-ro
```

The final command must show `ro` in the options. The sentinel spoliation check hashes `/mnt/case-ro` before and after execution.

## 4A. Run The CFReDS Hacking Case Pilot

Use this path to replace the current public-benchmark 0.000 anchor with recovered artifact evidence. It requires the SIFT VM because it calls `ewfverify`, `ewfmount`, Sleuth Kit, and RegRipper.

```bash
cd ~/kelp-claw
corepack enable
pnpm install --frozen-lockfile
pnpm -r --if-present build
node scripts/fetch-cfreds-hacking-case.mjs
node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --trace .kelpclaw/findevil/cfreds-hacking-case/triage/trace.jsonl \
  --max-iterations 3 \
  --timestamp skip \
  --out .kelpclaw/findevil/sentinel-cfreds-hacking-case
```

Expected output paths:

```text
.kelpclaw/findevil/cfreds-hacking-case/triage/trace.jsonl
.kelpclaw/findevil/cfreds-hacking-case/triage/evidence/artifacts/
.kelpclaw/findevil/sentinel-cfreds-hacking-case/accuracy-report.md
.kelpclaw/findevil/sentinel-cfreds-hacking-case/audit-bundle/
```

If `ewfmount` fails inside VMware Fusion, install FUSE support in the SIFT VM or run `ewfmount` manually and pass the exposed raw image:

```bash
mkdir -p /tmp/hacking-ewf
ewfmount ".kelpclaw/datasets/cfreds/hacking-case/4Dell Latitude CPi.E01" /tmp/hacking-ewf
node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --raw-image /tmp/hacking-ewf/ewf1 \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
```

## 4B. Start The Read-Only Find Evil MCP Server

This is the custom MCP architecture path. It gives Claude Code or Protocol SIFT typed functions instead of arbitrary shell:

```bash
./node_modules/.bin/kelp-claw findevil mcp \
  --evidence-root .kelpclaw/datasets/cfreds/hacking-case \
  --max-runtime-seconds 180
```

For Claude Code, use `examples/findevil-cfreds-hacking-case/mcp-config.example.json` as the MCP config template and keep the evidence directory read-only at the VM or mount layer.

## 5. Run Live Sentinel Mode

Create a Protocol SIFT wrapper that adapts the upstream `cd /cases/<CASE> && claude` contract into JSONL-producing CLI output:

```bash
sudo tee /usr/local/bin/protocol-sift >/dev/null <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" != "run" ]]; then
  echo "usage: protocol-sift run --case-dir DIR --output-jsonl" >&2
  exit 2
fi
case_dir=""
output_jsonl=false
shift
while [[ $# -gt 0 ]]; do
  case "$1" in
    --case-dir)
      case_dir="${2:-}"
      shift 2
      ;;
    --output-jsonl)
      output_jsonl=true
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done
if [[ -z "$case_dir" || "$output_jsonl" != true ]]; then
  echo "usage: protocol-sift run --case-dir DIR --output-jsonl" >&2
  exit 2
fi
case_id="KELP-FINDEVIL-LIVE"
mkdir -p "/cases/${case_id}"/{analysis,exports,reports}
cp -f ~/.claude/case-templates/CLAUDE.md "/cases/${case_id}/CLAUDE.md"
cd "/cases/${case_id}"
prompt="Use Protocol SIFT skills to triage the read-only evidence at ${case_dir}. Emit newline-delimited JSON objects with tool_call, tool_result, claim_extracted, and final_report events. Write derived artifacts only under ./analysis, ./exports, or ./reports."
exec claude -p "$prompt" --output-format stream-json --verbose
SH
sudo chmod +x /usr/local/bin/protocol-sift
```

Run KelpClaw from the repository root:

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm -r --if-present build
rm -rf /tmp/kelpclaw-findevil-sift-live
mkdir -p /tmp/kelpclaw-findevil-sift-live
./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --evidence-root /mnt/case-ro \
  --sift-command "protocol-sift run --case-dir /mnt/case-ro --output-jsonl" \
  --repair-runner claude-code \
  --max-iterations 3 \
  --out /tmp/kelpclaw-findevil-sift-live \
  | tee /tmp/kelpclaw-findevil-sift-live/sentinel-result.json
./node_modules/.bin/kelp-claw verify-audit-bundle \
  /tmp/kelpclaw-findevil-sift-live/audit-bundle \
  --profile reviewer
```

The process budget comes from `examples/findevil-sift-sentinel/case.yml`:

```yaml
siftIntegration:
  siftCommand: "protocol-sift run --case-dir /mnt/case-ro --output-jsonl"
  maxRuntimeSeconds: 900
```

## 6. Troubleshooting

- Claude Code rate limits: rerun after the reset window, reduce the case prompt scope, or use offline `--trace` mode for judge review.
- MCP or Protocol SIFT handshake fails: run `claude --version`, rerun `bash ~/protocol-sift/install.sh`, and confirm `~/.claude/CLAUDE.md`, `~/.claude/settings.json`, and `~/.claude/skills/` exist.
- Evidence root is not read-only: rerun `sudo mount -o remount,bind,ro /mnt/case-ro` and verify with `findmnt -no OPTIONS /mnt/case-ro`.
- Runtime budget exceeded: increase `siftIntegration.maxRuntimeSeconds` in the case file or narrow the prompt/wrapper to one artifact class.
- `agent-execution.jsonl` is empty: confirm the wrapper uses `claude -p ... --output-format stream-json --verbose` and that the sentinel process can write the `--out` directory.
- Hook path not visible to the child: print `env | grep KELP` inside the wrapper. The runner sets `KELP_AGENT_HOOK_PATH`, `KELP_AGENT_EXECUTION_JSONL`, and `KELPCLAW_AGENT_RUN_ID`.

## Recorded Live Run

No SIFT Workstation VM is attached to this workspace, so I cannot verify this section or paste a real live transcript without fabricating it. Capture it on the VM with:

```bash
cat /tmp/kelpclaw-findevil-sift-live/sentinel-result.json
sed -n '1,50p' /tmp/kelpclaw-findevil-sift-live/agent-execution.jsonl
```

Paste the full sentinel JSON output from the `kelp-claw findevil sentinel` command and the first 50 lines of `agent-execution.jsonl` here after the VM run.

## Captured live transcript

No SIFT Workstation VM is attached to this workspace, so this is not a live VM transcript.

Offline trace mode captured 2026-05-31. Live `--sift-command` integration was tested manually but no full transcript is captured for the canonical bundle. See limitations in the accuracy report.

Canonical offline trace transcript:

```console
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --evidence-root examples/findevil-sift-sentinel/case-data \
  --trace fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --out .kelpclaw/findevil/sentinel
$ cat .kelpclaw/findevil/sentinel/audit-bundle/result.json
{
  "ok": true,
  "runId": "findevil-sift-sentinel-demo-001-mpt33q3b",
  "status": "succeeded",
  "mode": "sentinel",
  "policyDenials": 1,
  "uncorrectedPolicyDenials": 0
}
$ sed -n '1,12p' .kelpclaw/findevil/sentinel/accuracy-report.md
# KelpClaw Find Evil Accuracy Report

## Summary

- Baseline claims: 10
- Repaired claims: 10
- Repair prompts: 11
- Repair results: 11
- Successful status changes: 5
- Firewall blocks: 1
$ sed -n '1,5p' .kelpclaw/findevil/sentinel/agent-execution.jsonl
{"sourceAgent":"claude-code","sessionId":"protocol-sift-baseline-demo-002","hookEvent":"Notification","toolName":"ProtocolSIFT","args":{"traceEvent":{"event":"run_started","timestamp":"2026-05-30T00:00:00.000Z","runId":"protocol-sift-baseline-demo-002","case":"examples/findevil-sift-sentinel/case.yml","agent":"Protocol SIFT synthetic baseline"}},"status":"succeeded","startedAt":"2026-05-31T01:11:40.384Z","rawEvent":{"event":"run_started","timestamp":"2026-05-30T00:00:00.000Z","runId":"protocol-sift-baseline-demo-002","case":"examples/findevil-sift-sentinel/case.yml","agent":"Protocol SIFT synthetic baseline"}}
{"sourceAgent":"claude-code","sessionId":"protocol-sift-baseline-demo-002","hookEvent":"PreToolUse","toolName":"filesystem.read_file","toolUseId":"call-001","args":{"path":"examples/findevil-sift-sentinel/case-data/timeline.csv","purpose":"Load Plaso-style timeline rows for suspicious execution, persistence, network, and malware triage."},"status":"pending","startedAt":"2026-05-31T01:11:40.384Z","rawEvent":{"event":"tool_call","timestamp":"2026-05-30T00:00:02.000Z","runId":"protocol-sift-baseline-demo-002","callId":"call-001","tool":"filesystem.read_file","arguments":{"path":"examples/findevil-sift-sentinel/case-data/timeline.csv","purpose":"Load Plaso-style timeline rows for suspicious execution, persistence, network, and malware triage."}}}
{"sourceAgent":"claude-code","sessionId":"protocol-sift-baseline-demo-002","hookEvent":"PostToolUse","toolName":"filesystem.read_file","toolUseId":"call-001","args":{"callId":"call-001"},"result":{"event":"tool_result","timestamp":"2026-05-30T00:00:02.400Z","runId":"protocol-sift-baseline-demo-002","callId":"call-001","status":"ok","summary":"Read timeline.csv and observed rows 1848-1856 for PowerShell execution, invoice_viewer file presence, Run-key persistence, TaskCache reference, PCAP flow, DNS lookup, credential access indicator, and YARA hit."},"status":"succeeded","startedAt":"2026-05-31T01:11:40.399Z","finishedAt":"2026-05-31T01:11:40.399Z","rawEvent":{"event":"tool_result","timestamp":"2026-05-30T00:00:02.400Z","runId":"protocol-sift-baseline-demo-002","callId":"call-001","status":"ok","summary":"Read timeline.csv and observed rows 1848-1856 for PowerShell execution, invoice_viewer file presence, Run-key persistence, TaskCache reference, PCAP flow, DNS lookup, credential access indicator, and YARA hit."}}
{"sourceAgent":"claude-code","sessionId":"protocol-sift-baseline-demo-002","hookEvent":"PreToolUse","toolName":"filesystem.read_file","toolUseId":"call-002","args":{"path":"examples/findevil-sift-sentinel/case-data/prefetch/POWERSHELL.EXE-A9B4C2D1.json","purpose":"Inspect parsed Prefetch evidence for PowerShell execution."},"status":"pending","startedAt":"2026-05-31T01:11:40.399Z","rawEvent":{"event":"tool_call","timestamp":"2026-05-30T00:00:03.000Z","runId":"protocol-sift-baseline-demo-002","callId":"call-002","tool":"filesystem.read_file","arguments":{"path":"examples/findevil-sift-sentinel/case-data/prefetch/POWERSHELL.EXE-A9B4C2D1.json","purpose":"Inspect parsed Prefetch evidence for PowerShell execution."}}}
{"sourceAgent":"claude-code","sessionId":"protocol-sift-baseline-demo-002","hookEvent":"PostToolUse","toolName":"filesystem.read_file","toolUseId":"call-002","args":{"callId":"call-002"},"result":{"event":"tool_result","timestamp":"2026-05-30T00:00:03.250Z","runId":"protocol-sift-baseline-demo-002","callId":"call-002","status":"ok","summary":"Prefetch summary shows POWERSHELL.EXE runCount=1 lastRun=2026-02-18T09:36:05Z."},"status":"succeeded","startedAt":"2026-05-31T01:11:40.401Z","finishedAt":"2026-05-31T01:11:40.401Z","rawEvent":{"event":"tool_result","timestamp":"2026-05-30T00:00:03.250Z","runId":"protocol-sift-baseline-demo-002","callId":"call-002","status":"ok","summary":"Prefetch summary shows POWERSHELL.EXE runCount=1 lastRun=2026-02-18T09:36:05Z."}}
```
