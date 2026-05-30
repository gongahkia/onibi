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
5. Import the VM into VMware or VirtualBox and boot it.
6. Default login:
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
