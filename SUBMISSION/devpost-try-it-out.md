# Try It Out

These commands were run against the current repository state. They use the deterministic offline Protocol SIFT-style fixture and write fresh outputs to `/tmp/kelpclaw-findevil-sentinel` so the committed `.kelpclaw/findevil/sentinel/` run stays unchanged for review.

Equivalent invocation: `pnpm exec kelp-claw ...`.

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
