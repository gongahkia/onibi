# Try It Out

Public repository: `https://github.com/gongahkia/kelp-claw`

Demo video artifact: `SUBMISSION/kelpclaw-sift-sentinel-demo.mp4`

Use the published container to run the deterministic offline Protocol SIFT-style fixture. It mounts the case bundle read-only and writes fresh reviewer outputs to `.kelpclaw/findevil/sentinel/`.

```bash
docker run -v $PWD/examples/findevil-sift-sentinel/case-data:/data/case/case-data:ro \
           -v $PWD/examples/findevil-sift-sentinel/case.yml:/data/case/case.yml:ro \
           -v $PWD/.kelpclaw/findevil/sentinel:/data/out \
           ghcr.io/gongahkia/kelp-claw:latest \
           findevil sentinel --case /data/case/case.yml \
                              --evidence-root /data/case/case-data \
                              --trace /app/fixtures/protocol-sift-baseline/baseline.jsonl \
                              --max-iterations 3 \
                              --out /data/out
```

Live SIFT Workstation mode is available for reviewers with the VM; deterministic offline `--trace` mode is the fallback source-build path.

## Live SIFT Workstation Mode

Run this inside the SIFT Workstation VM after following `docs/sift-workstation-setup.md`:

```console
$ git clone https://github.com/gongahkia/kelp-claw.git
$ cd kelp-claw
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm -r --if-present build
$ sudo mkdir -p /mnt/case-source /mnt/case-ro
$ sudo mount --bind "$PWD/examples/findevil-sift-sentinel/case-data" /mnt/case-source
$ sudo mount --bind /mnt/case-source /mnt/case-ro
$ sudo mount -o remount,bind,ro /mnt/case-ro
$ rm -rf /tmp/kelpclaw-findevil-sift-live
$ mkdir -p /tmp/kelpclaw-findevil-sift-live
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --evidence-root /mnt/case-ro \
  --sift-command "protocol-sift run --case-dir /mnt/case-ro --output-jsonl" \
  --max-iterations 3 \
  --out /tmp/kelpclaw-findevil-sift-live \
  | tee /tmp/kelpclaw-findevil-sift-live/sentinel-result.json
$ sed -n '1,50p' /tmp/kelpclaw-findevil-sift-live/agent-execution.jsonl
$ ./node_modules/.bin/kelp-claw verify-audit-bundle /tmp/kelpclaw-findevil-sift-live/audit-bundle --profile reviewer
```

The live runtime budget is `siftIntegration.maxRuntimeSeconds: 900` in `examples/findevil-sift-sentinel/case.yml`.

## Offline Trace Fallback

```console
$ corepack enable
$ pnpm install --frozen-lockfile
$ pnpm -r --if-present build
$ rm -rf /tmp/kelpclaw-findevil-sentinel
$ ./node_modules/.bin/kelp-claw findevil sentinel \
  --case examples/findevil-sift-sentinel/case.yml \
  --evidence-root examples/findevil-sift-sentinel/case-data \
  --trace fixtures/protocol-sift-baseline/baseline.jsonl \
  --max-iterations 3 \
  --out /tmp/kelpclaw-findevil-sentinel
$ sed -n '1,80p' /tmp/kelpclaw-findevil-sentinel/accuracy-report.md
$ jq '{ok, checkedAt, changed:(.changed|length), added:(.added|length), removed:(.removed|length)}' /tmp/kelpclaw-findevil-sentinel/spoliation-check.json
$ wc -l /tmp/kelpclaw-findevil-sentinel/{agent-execution,committee-vote,repair-trace,firewall-events,taint-ledger}.jsonl
$ test -s /tmp/kelpclaw-findevil-sentinel/accuracy-report.md && test -s /tmp/kelpclaw-findevil-sentinel/audit-bundle/index.html
$ ./node_modules/.bin/kelp-claw verify-audit-bundle /tmp/kelpclaw-findevil-sentinel/audit-bundle --profile reviewer
```

Expected high-level result:

- The sentinel command returns `ok: true`, `status: "succeeded"`, `policyDenials: 1`, and `uncorrectedPolicyDenials: 0`.
- The accuracy report shows 10 baseline claims, 10 repaired claims, 11 repair prompts, 11 repair results, 5 successful status changes, and 1 firewall block.
- The benchmark table shows 10 expected findings, 10 evaluated claims, 3 true positives, 0 false positives, 7 false negatives, precision 1.000, recall 0.300, and F1 0.462.
- The ATT&CK table covers T1003, T1021, T1059, T1071, T1204, and T1547.
- The spoliation check shows `ok: true` with 13 files before, 13 files after, and zero changed, added, or removed files.
- The audit-bundle verification returns `ok: true` with a valid reviewer signature and 18 checked files.
