#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
ollama_bin="${KELP_PI_OLLAMA_BIN:-ollama}"
model="${KELP_PI_OLLAMA_MODEL:-qwen2.5:0.5b}"
expect="load"
output_file=""

fail() {
  printf 'FAIL %s\n' "$*" >&2
  [ -z "$output_file" ] || sed -n '1,120p' "$output_file" >&2
  exit 1
}

pass() {
  printf 'OK %s\n' "$*"
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 missing"
}

cleanup() {
  [ -z "$output_file" ] || rm -f "$output_file"
}
trap cleanup EXIT INT TERM

while [ $# -gt 0 ]; do
  case "$1" in
    --agent-bin)
      [ $# -ge 2 ] || fail "--agent-bin requires a value"
      agent_bin="$2"
      shift 2
      ;;
    --ollama-bin)
      [ $# -ge 2 ] || fail "--ollama-bin requires a value"
      ollama_bin="$2"
      shift 2
      ;;
    --model)
      [ $# -ge 2 ] || fail "--model requires a value"
      model="$2"
      shift 2
      ;;
    --expect-load)
      expect="load"
      shift
      ;;
    --expect-refuse)
      expect="refuse"
      shift
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 64
      ;;
  esac
done

need "$agent_bin"
need awk
need grep
need mktemp
need rm
need sed

json_number() {
  awk -v key="\"$1\"" '
    index($0, key) {
      sub(/.*:[[:space:]]*/, "", $0)
      sub(/,.*/, "", $0)
      gsub(/[[:space:]]/, "", $0)
      print $0
      exit
    }
  ' "$output_file"
}

require_pi_ram() {
  grep -Eq '"raspberry_pi"[[:space:]]*:[[:space:]]*true' "$output_file" || fail "Ollama proof is not from a Raspberry Pi"
  ram_bytes="$(json_number ram_bytes)"
  min_ram_bytes="$(json_number min_pi_ram_bytes)"
  [ -n "$ram_bytes" ] || fail "Ollama RAM proof missing ram_bytes"
  [ -n "$min_ram_bytes" ] || fail "Ollama RAM proof missing min_pi_ram_bytes"
  case "$ram_bytes:$min_ram_bytes" in
    *[!0-9:]* | :* | *:) fail "Ollama RAM proof is not numeric" ;;
  esac
  printf 'OK Ollama hardware proof raspberry_pi=true model=%s ram_bytes=%s min_pi_ram_bytes=%s\n' "$model" "$ram_bytes" "$min_ram_bytes"
}

output_file="$(mktemp)"
status=0
"$agent_bin" ollama load-check --enable-ollama --model "$model" --ollama-bin "$ollama_bin" >"$output_file" 2>&1 || status="$?"

case "$expect" in
  load)
    [ "$status" = "0" ] || fail "Ollama load-check failed with exit $status"
    grep -Eq '"decision"[[:space:]]*:[[:space:]]*"allow"' "$output_file" || fail "Ollama guard did not allow load"
    grep -Eq '"status"[[:space:]]*:[[:space:]]*"loaded"' "$output_file" || fail "Ollama model did not load"
    require_pi_ram
    awk -v ram="$ram_bytes" -v min="$min_ram_bytes" 'BEGIN { exit !(ram >= min) }' || fail "Ollama load proof RAM is below selected model minimum"
    pass "Ollama loaded $model on Raspberry Pi ram_bytes=$ram_bytes min_pi_ram_bytes=$min_ram_bytes"
    ;;
  refuse)
    [ "$status" = "77" ] || fail "Ollama refusal expected exit 77, got $status"
    grep -Eq '"decision"[[:space:]]*:[[:space:]]*"refuse"' "$output_file" || fail "Ollama guard did not refuse"
    grep -Eq '"status"[[:space:]]*:[[:space:]]*"skipped"' "$output_file" || fail "Ollama load was not skipped"
    require_pi_ram
    awk -v ram="$ram_bytes" -v min="$min_ram_bytes" 'BEGIN { exit !(ram < min) }' || fail "Ollama refusal proof RAM is not below selected model minimum"
    pass "Ollama refused $model on Raspberry Pi ram_bytes=$ram_bytes min_pi_ram_bytes=$min_ram_bytes before load"
    ;;
  *)
    fail "unknown expectation: $expect"
    ;;
esac
