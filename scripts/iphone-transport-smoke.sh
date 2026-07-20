#!/usr/bin/env bash
set -euo pipefail
umask 077

transport=""
pair_url=""
out_dir="${ONIBI_IPHONE_SMOKE_OUT:-}"
skip_local=false
allow_skip=false
setup_health=""
pairing=""
approval=""
intervention=""
reconnect=""
teardown=""
failure_diagnostics=""

usage() {
  cat >&2 <<'EOF'
usage: scripts/iphone-transport-smoke.sh --transport <mode> --pair-url <https-url> --out <dir> [options]

Records structured, token-redacted evidence for a physical iPhone transport smoke.
The Onibi service must already be running; this script never starts it or handles credentials.

Options:
  --transport <mode>       lan, tailscale-private, wireguard, zerotier, cloudflare-quick, or ngrok
  --pair-url <https-url>   active owner pairing URL from Onibi output; the token is never written to evidence
  --out <dir>              evidence directory; required unless ONIBI_IPHONE_SMOKE_OUT is set
  --record <check=status>  non-interactive result for setup_health, pairing, approval, intervention, reconnect, teardown, or failure_diagnostics
  --skip-local-checks      do not run host-side Go verification
  --allow-skip             permit skipped physical checkpoints without a failing exit status

The script writes one 0600 JSON artifact and returns non-zero for a failed or incomplete smoke.
EOF
}

valid_transport() {
  case "$1" in
    lan|tailscale-private|wireguard|zerotier|cloudflare-quick|ngrok) return 0 ;;
    *) return 1 ;;
  esac
}

valid_check() {
  case "$1" in
    setup_health|pairing|approval|intervention|reconnect|teardown|failure_diagnostics) return 0 ;;
    *) return 1 ;;
  esac
}

valid_status() {
  case "$1" in
    pass|fail|skip) return 0 ;;
    *) return 1 ;;
  esac
}

normalize_status() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

set_record() {
  case "$1" in
    setup_health) setup_health="$2" ;;
    pairing) pairing="$2" ;;
    approval) approval="$2" ;;
    intervention) intervention="$2" ;;
    reconnect) reconnect="$2" ;;
    teardown) teardown="$2" ;;
    failure_diagnostics) failure_diagnostics="$2" ;;
  esac
}

get_record() {
  case "$1" in
    setup_health) printf '%s' "$setup_health" ;;
    pairing) printf '%s' "$pairing" ;;
    approval) printf '%s' "$approval" ;;
    intervention) printf '%s' "$intervention" ;;
    reconnect) printf '%s' "$reconnect" ;;
    teardown) printf '%s' "$teardown" ;;
    failure_diagnostics) printf '%s' "$failure_diagnostics" ;;
  esac
}

while (($#)); do
  case "$1" in
    --transport) transport="${2:-}"; shift 2 ;;
    --transport=*) transport="${1#*=}"; shift ;;
    --pair-url) pair_url="${2:-}"; shift 2 ;;
    --pair-url=*) pair_url="${1#*=}"; shift ;;
    --out) out_dir="${2:-}"; shift 2 ;;
    --out=*) out_dir="${1#*=}"; shift ;;
    --record)
      record="${2:-}"
      check="${record%%=*}"
      status="${record#*=}"
      status="$(normalize_status "$status")"
      if [[ "$check" == "$record" ]] || ! valid_check "$check" || ! valid_status "$status"; then
        echo "invalid --record: $record" >&2
        exit 2
      fi
      set_record "$check" "$status"
      shift 2
      ;;
    --record=*)
      record="${1#*=}"
      check="${record%%=*}"
      status="${record#*=}"
      status="$(normalize_status "$status")"
      if [[ "$check" == "$record" ]] || ! valid_check "$check" || ! valid_status "$status"; then
        echo "invalid --record: $record" >&2
        exit 2
      fi
      set_record "$check" "$status"
      shift
      ;;
    --skip-local-checks) skip_local=true; shift ;;
    --allow-skip) allow_skip=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if ! valid_transport "$transport"; then
  echo "unsupported or missing transport: $transport" >&2
  exit 2
fi
if [[ -z "$out_dir" ]]; then
  echo "--out or ONIBI_IPHONE_SMOKE_OUT is required" >&2
  exit 2
fi
if [[ "$pair_url" != https://* ]]; then
  echo "--pair-url must be an HTTPS owner pairing URL" >&2
  exit 2
fi
pair_without_fragment="${pair_url%%#*}"
pair_host_and_path="${pair_without_fragment#https://}"
pair_host="${pair_host_and_path%%/*}"
pair_path="/${pair_host_and_path#*/}"
host_valid=false
if printf '%s' "$pair_host" | LC_ALL=C grep -Eq '^[[:alnum:].:_-]+$' || printf '%s' "$pair_host" | LC_ALL=C grep -Eq '^\[[0-9A-Fa-f:.]+\](:[0-9]+)?$'; then
  host_valid=true
fi
if [[ -z "$pair_host" ]] || ! "$host_valid" || [[ "$pair_path" != /pair/* ]] || [[ "${pair_path#/pair/}" == "$pair_path" ]] || [[ -z "${pair_path#/pair/}" ]]; then
  echo "--pair-url must be an HTTPS owner pairing URL" >&2
  exit 2
fi

origin="https://$pair_host"
mkdir -p "$out_dir"
artifact="$out_dir/iphone-transport-${transport}-$(date -u +%Y%m%dT%H%M%SZ).json"
started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
local_status="skip"

if ! "$skip_local"; then
  if go test -race ./internal/e2e ./internal/web; then
    local_status="pass"
  else
    local_status="fail"
  fi
fi

prompt_check() {
  check="$1"
  prompt="$2"
  if [[ -n "$(get_record "$check")" ]]; then
    return
  fi
  if [[ ! -t 0 ]]; then
    set_record "$check" "skip"
    return
  fi
  while true; do
    read -r -p "[$check] $prompt [pass/fail/skip]: " status
    status="$(normalize_status "$status")"
    if valid_status "$status"; then
      set_record "$check" "$status"
      return
    fi
    echo "enter pass, fail, or skip" >&2
  done
}

prompt_check setup_health "Open the QR URL in iPhone Safari, confirm the cockpit loads and health is current."
prompt_check pairing "Pair as owner and confirm the owner session is established."
prompt_check approval "Trigger one approval and confirm approve or deny reaches the host exactly once."
prompt_check intervention "Send one short input or interrupt and confirm the remote command status is acknowledged."
prompt_check reconnect "Disrupt the selected transport, restore it, reconnect, and confirm no duplicate input."
prompt_check teardown "Stop Onibi and confirm the transport cleanup and old URL behavior."
prompt_check failure_diagnostics "Trigger one expected transport failure and confirm an actionable diagnostic is shown."

failed=0
skipped=0
for check in setup_health pairing approval intervention reconnect teardown failure_diagnostics; do
  case "$(get_record "$check")" in
    fail) failed=1 ;;
    skip) skipped=1 ;;
  esac
done
if [[ "$local_status" == "fail" ]]; then
  failed=1
fi
if [[ "$local_status" == "skip" ]]; then
  skipped=1
fi
complete=true
if ((failed || skipped)); then
  complete=false
fi
finished="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

cat >"$artifact" <<EOF
{
  "schema": "onibi.iphone-transport-smoke.v1",
  "transport": "$transport",
  "origin": "$origin",
  "started": "$started",
  "finished": "$finished",
  "local_verification": "$local_status",
  "complete": $complete,
  "checks": [
    {"name":"setup_health","status":"$setup_health"},
    {"name":"pairing","status":"$pairing"},
    {"name":"approval","status":"$approval"},
    {"name":"intervention","status":"$intervention"},
    {"name":"reconnect","status":"$reconnect"},
    {"name":"teardown","status":"$teardown"},
    {"name":"failure_diagnostics","status":"$failure_diagnostics"}
  ]
}
EOF
chmod 0600 "$artifact"
printf 'artifact: %s\n' "$artifact"

if ((failed)); then
  exit 1
fi
if ((skipped)) && ! "$allow_skip"; then
  exit 1
fi
