#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
out="${1:-$repo_root/.kelpclaw/pi-vulnerable-target-walkthrough}"
data_dir="$out/data"
workspace="$out/workspace"
bundle="$out/audit-bundle"
agent="$repo_root/packages/pi-agent/target/debug/kelp-pi-agent"
raw="$repo_root/examples/pi-vulnerable-target/fixtures/nuclei.jsonl"
cli="$repo_root/packages/cli/dist/index.js"

rm -rf "$out"
mkdir -p "$data_dir" "$workspace/raw"
for dir in corpus evidence bundles index audit keys policy scope; do
  mkdir -p "$data_dir/$dir"
done

cargo build --manifest-path "$repo_root/packages/pi-agent/Cargo.toml" --quiet
"$agent" keygen --data-dir "$data_dir" --label fixture-walkthrough > "$out/key.json"
"$agent" policy-check --data-dir "$data_dir" --gate outbound-network-request --host fixture.local --allowed > "$out/policy.json"
"$agent" normalize nuclei --data-dir "$data_dir" --input "$raw" --workspace "$workspace" --raw-path raw/nuclei.jsonl --min-free-bytes 1 > "$out/normalize.json"
"$agent" bundle assemble --data-dir "$data_dir" --workspace "$workspace" --output "$bundle" --run-id fixture-target > "$out/assembly.json"
pnpm --filter @kelpclaw/cli... build >/dev/null
node "$cli" verify-audit-bundle "$bundle" --profile reviewer > "$out/verification.json"

printf '%s\n' "$out"
