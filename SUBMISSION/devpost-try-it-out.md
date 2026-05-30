# Try It Out

Live SIFT Workstation mode is the preferred judge path; deterministic offline `--trace` mode is the fallback for reviewers who do not have the VM. The offline fallback commands below were run against the current repository state and write fresh outputs to `/tmp/kelpclaw-findevil-sentinel` so the committed `.kelpclaw/findevil/sentinel/` run stays unchanged for review.

Equivalent invocation: `pnpm exec kelp-claw ...`.

## Live SIFT Workstation Mode

Run this inside the SIFT Workstation VM after following `docs/sift-workstation-setup.md`:

```console
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
$ wc -l /tmp/kelpclaw-findevil-sentinel/{agent-execution,repair-trace,firewall-events,taint-ledger}.jsonl
$ test -s /tmp/kelpclaw-findevil-sentinel/accuracy-report.md && test -s /tmp/kelpclaw-findevil-sentinel/audit-bundle/index.html
$ ./node_modules/.bin/kelp-claw verify-audit-bundle /tmp/kelpclaw-findevil-sentinel/audit-bundle --profile reviewer
```

Expected high-level result:

- The sentinel command returns `ok: true`, `status: "succeeded"`, `policyDenials: 1`, and `uncorrectedPolicyDenials: 0`.
- The accuracy report shows one baseline claim, one repaired claim, one repair prompt, one repair result, one successful status change, and one firewall block.
- The spoliation check shows `ok: true` with zero changed, added, or removed files.
- The audit-bundle verification returns `ok: true` with a valid reviewer signature and thirteen checked files.
