#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
out="$repo_root/.kelpclaw/pi-vulnerable-target-field"
pi_host=""
pi_user="${KELP_PI_SSH_USER:-kelp-pi}"
ssh_bin="${KELP_PI_SSH_BIN:-ssh}"
scp_bin="${KELP_PI_SCP_BIN:-scp}"
data_dir="${KELP_PI_DATA_DIR:-/var/lib/kelp-pi}"
run_id="fixture-target"
scope_id="fixture-target-scope"
until=""
fixture_url="http://fixture.local:8080"
fixture_ip=""
control_url=""
current_config="/etc/kelp-pi/network-hardening.json"
updated_config=""
client_a=""
client_b=""
client_ssh_user=""
upstream_interface=""
dns_probe_command="for d in captive.apple.com connectivitycheck.gstatic.com clients3.google.com; do printf '%s ' \"\$d\"; dig +short @10.42.0.1 \"\$d\"; done"
wan_forbidden_ip=""
ollama_check=""
ollama_model=""
readonly_root=0
readonly_data_dir="$data_dir"
readonly_data_dir_explicit=0
max_seconds=1800

usage() {
  printf '%s\n' "usage: $0 --pi-host HOST --fixture-ip IP --control-url URL --client-a IP --client-b IP --client-ssh-user USER --wan-forbidden-ip IP --upstream-interface IFACE --updated-config PATH --until RFC3339 [--out DIR] [--max-seconds N] [--ollama-check load|refuse] [--readonly-root]"
}

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

json_field() {
  node -e 'const fs=require("node:fs"); const data=JSON.parse(fs.readFileSync(0,"utf8")); const value=process.argv.slice(1).reduce((acc,key)=>acc && acc[key], data); if (typeof value !== "string") process.exit(1); process.stdout.write(value);' "$1"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --out)
      [ $# -ge 2 ] || fail "--out requires a value"
      out="$2"
      shift 2
      ;;
    --pi-host)
      [ $# -ge 2 ] || fail "--pi-host requires a value"
      pi_host="$2"
      shift 2
      ;;
    --pi-user)
      [ $# -ge 2 ] || fail "--pi-user requires a value"
      pi_user="$2"
      shift 2
      ;;
    --data-dir)
      [ $# -ge 2 ] || fail "--data-dir requires a value"
      data_dir="$2"
      readonly_data_dir="$2"
      shift 2
      ;;
    --run-id)
      [ $# -ge 2 ] || fail "--run-id requires a value"
      run_id="$2"
      shift 2
      ;;
    --scope-id)
      [ $# -ge 2 ] || fail "--scope-id requires a value"
      scope_id="$2"
      shift 2
      ;;
    --until)
      [ $# -ge 2 ] || fail "--until requires a value"
      until="$2"
      shift 2
      ;;
    --fixture-url)
      [ $# -ge 2 ] || fail "--fixture-url requires a value"
      fixture_url="$2"
      shift 2
      ;;
    --fixture-ip)
      [ $# -ge 2 ] || fail "--fixture-ip requires a value"
      fixture_ip="$2"
      shift 2
      ;;
    --control-url)
      [ $# -ge 2 ] || fail "--control-url requires a value"
      control_url="$2"
      shift 2
      ;;
    --current-config)
      [ $# -ge 2 ] || fail "--current-config requires a value"
      current_config="$2"
      shift 2
      ;;
    --updated-config)
      [ $# -ge 2 ] || fail "--updated-config requires a value"
      updated_config="$2"
      shift 2
      ;;
    --client-a)
      [ $# -ge 2 ] || fail "--client-a requires a value"
      client_a="$2"
      shift 2
      ;;
    --client-b)
      [ $# -ge 2 ] || fail "--client-b requires a value"
      client_b="$2"
      shift 2
      ;;
    --client-ssh-user)
      [ $# -ge 2 ] || fail "--client-ssh-user requires a value"
      client_ssh_user="$2"
      shift 2
      ;;
    --upstream-interface)
      [ $# -ge 2 ] || fail "--upstream-interface requires a value"
      upstream_interface="$2"
      shift 2
      ;;
    --dns-probe-command)
      [ $# -ge 2 ] || fail "--dns-probe-command requires a value"
      dns_probe_command="$2"
      shift 2
      ;;
    --wan-forbidden-ip)
      [ $# -ge 2 ] || fail "--wan-forbidden-ip requires a value"
      wan_forbidden_ip="$2"
      shift 2
      ;;
    --ollama-check)
      [ $# -ge 2 ] || fail "--ollama-check requires a value"
      case "$2" in
        load|refuse)
          ollama_check="$2"
          ;;
        *)
          fail "--ollama-check must be load or refuse"
          ;;
      esac
      shift 2
      ;;
    --ollama-model)
      [ $# -ge 2 ] || fail "--ollama-model requires a value"
      ollama_model="$2"
      shift 2
      ;;
    --readonly-root)
      readonly_root=1
      shift
      ;;
    --readonly-data-dir)
      [ $# -ge 2 ] || fail "--readonly-data-dir requires a value"
      readonly_data_dir="$2"
      readonly_data_dir_explicit=1
      shift 2
      ;;
    --max-seconds)
      [ $# -ge 2 ] || fail "--max-seconds requires a value"
      max_seconds="$2"
      shift 2
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

[ -n "$pi_host" ] || fail "requires --pi-host"
[ -n "$fixture_ip" ] || fail "requires --fixture-ip"
[ -n "$control_url" ] || fail "requires --control-url"
[ -n "$client_a" ] || fail "requires --client-a"
[ -n "$client_b" ] || fail "requires --client-b"
[ -n "$client_ssh_user" ] || fail "requires --client-ssh-user"
[ -n "$wan_forbidden_ip" ] || fail "requires --wan-forbidden-ip"
[ -n "$upstream_interface" ] || fail "requires --upstream-interface"
[ -n "$updated_config" ] || fail "requires --updated-config"
[ -n "$until" ] || fail "requires --until"
[ -z "$ollama_model" ] || [ -n "$ollama_check" ] || fail "--ollama-model requires --ollama-check"
[ "$readonly_root" = "1" ] || [ "$readonly_data_dir_explicit" = "0" ] || fail "--readonly-data-dir requires --readonly-root"
case "$max_seconds" in
  ''|*[!0-9]*)
    fail "--max-seconds must be a non-negative integer"
    ;;
esac

need node
need pnpm
need "$ssh_bin"
need "$scp_bin"
need date

rm -rf "$out"
mkdir -p "$out"
start_epoch="$(date -u '+%s')"
started_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
agent_wrapper="$out/kelp-pi-agent-ssh"
cat >"$agent_wrapper" <<EOF
#!/usr/bin/env sh
exec "$ssh_bin" "$pi_user@$pi_host" kelp-pi-agent "\$@"
EOF
chmod 0755 "$agent_wrapper"

pnpm --filter @kelpclaw/cli... build >/dev/null
cli="$repo_root/packages/cli/dist/index.js"
bundle_dir="$out/fetched-bundle"
field_dir="$out/field-acceptance"
remote_field_dir="$data_dir/bundles/$run_id-field-acceptance"
remote_workspace="$data_dir/evidence/$run_id"
remote_raw="$remote_workspace/raw/nuclei.jsonl"

node "$cli" pi scope set \
  --url "$fixture_url" \
  --scope-id "$scope_id" \
  --until "$until" \
  --data-dir "$data_dir" \
  --agent-bin "$agent_wrapper" >"$out/scope.json"

"$ssh_bin" "$pi_user@$pi_host" \
  kelp-pi-agent approval-request \
    --data-dir "$data_dir" \
    --gate scanner-invocation \
    --scope-id "$scope_id" \
    --command "scan nuclei $fixture_url" \
    --host "$fixture_url" \
    --allowed >"$out/approval-request.json"
approval_token="$(json_field token <"$out/approval-request.json")"
node "$cli" pi approve "$approval_token" --data-dir "$data_dir" --agent-bin "$agent_wrapper" >"$out/approval.json"

"$ssh_bin" "$pi_user@$pi_host" mkdir -p "$remote_workspace/raw"
"$ssh_bin" "$pi_user@$pi_host" \
  kelp-pi-agent scan nuclei \
    --data-dir "$data_dir" \
    --sandbox \
    --target "$fixture_url" \
    --scanner-target-ip "$fixture_ip" \
    --approval-token "$approval_token" \
    --run-id "$run_id" \
    -- \
    -jsonl \
    -o "$remote_raw" >"$out/scan.log" 2>&1
"$ssh_bin" "$pi_user@$pi_host" \
  kelp-pi-agent normalize nuclei \
    --data-dir "$data_dir" \
    --input "$remote_raw" \
    --workspace "$remote_workspace" \
    --raw-path raw/nuclei.jsonl >"$out/normalize.json"
"$ssh_bin" "$pi_user@$pi_host" \
  kelp-pi-agent index ingest \
    --data-dir "$data_dir" \
    --input "$remote_workspace/normalized/findings.json" \
    --path "evidence/$run_id/normalized-findings.json" >"$out/index.json"
"$ssh_bin" "$pi_user@$pi_host" \
  kelp-pi-agent ask \
    --data-dir "$data_dir" \
    --top-k 1 \
    default admin marker >"$out/ask.json"

set -- "$ssh_bin" "$pi_user@$pi_host" sudo kelp-pi-validate-field-acceptance \
  --output-dir "$remote_field_dir" \
  --target-ip "$fixture_ip" \
  --target-url "$fixture_url" \
  --nuclei-approval-token "$approval_token" \
  --control-url "$control_url" \
  --current-config "$current_config" \
  --updated-config "$updated_config" \
  --session-command "while sleep 5; do curl -fsS --max-time 5 $control_url >/dev/null || exit 1; done" \
  --client-a "$client_a" \
  --client-b "$client_b" \
  --ssh-user "$client_ssh_user" \
  --upstream-interface "$upstream_interface" \
  --dns-probe-command "$dns_probe_command" \
  --forbidden-ip "$wan_forbidden_ip" \
  --max-seconds "$max_seconds"
[ -z "$ollama_check" ] || set -- "$@" "--ollama-expect-$ollama_check"
[ -z "$ollama_model" ] || set -- "$@" --ollama-model "$ollama_model"
[ "$readonly_root" = "0" ] || set -- "$@" --readonly-root --readonly-data-dir "$readonly_data_dir"
"$@" >"$out/field-acceptance.log" 2>&1

"$ssh_bin" "$pi_user@$pi_host" \
  kelp-pi-agent bundle assemble \
    --data-dir "$data_dir" \
    --workspace "$remote_workspace" \
    --output "$data_dir/bundles/$run_id" \
    --run-id "$run_id" >"$out/assembly.json"

node "$cli" pi bundle fetch \
  --bundle-id "$run_id" \
  --run-id "$run_id" \
  --out "$bundle_dir" \
  --data-dir "$data_dir" \
  --agent-bin "$agent_wrapper" >"$out/fetch.json"
node "$cli" verify-audit-bundle "$bundle_dir" --profile reviewer >"$out/verification.json"

mkdir -p "$field_dir"
"$scp_bin" -r "$pi_user@$pi_host:$remote_field_dir/." "$field_dir/" >/dev/null
"$repo_root/scripts/verify-pi-field-acceptance.sh" "$field_dir" >"$out/field-verification.log"

end_epoch="$(date -u '+%s')"
duration_seconds=$((end_epoch - start_epoch))
{
  printf 'started_at=%s\n' "$started_at"
  printf 'finished_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf 'duration_seconds=%s\n' "$duration_seconds"
  printf 'max_seconds=%s\n' "$max_seconds"
} >"$out/timing.txt"
[ "$duration_seconds" -le "$max_seconds" ] || fail "field walkthrough exceeded ${max_seconds}s"

printf '%s\n' "$out"
