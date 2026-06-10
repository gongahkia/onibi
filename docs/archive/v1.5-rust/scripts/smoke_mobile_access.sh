#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${ONIBI_BASE_URL:-http://127.0.0.1:8787}"
RUN_REALTIME=1

usage() {
  cat <<'USAGE'
usage: scripts/smoke_mobile_access.sh [--base-url URL] [--skip-realtime]

Requires:
  ONIBI_PAIRING_TOKEN   Pairing token from host settings

Optional:
  ONIBI_BASE_URL        Default base URL (if --base-url omitted)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base-url)
      shift
      BASE_URL="${1:-}"
      ;;
    --skip-realtime)
      RUN_REALTIME=0
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument '$1'" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift || true
done

if [[ -z "${ONIBI_PAIRING_TOKEN:-}" ]]; then
  echo "error: ONIBI_PAIRING_TOKEN is required" >&2
  usage >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required" >&2
  exit 1
fi

if [[ "${RUN_REALTIME}" -eq 1 ]] && ! command -v swift >/dev/null 2>&1; then
  echo "error: swift is required when realtime probe is enabled" >&2
  exit 1
fi

auth_header="Authorization: Bearer ${ONIBI_PAIRING_TOKEN}"
bootstrap_path="$(mktemp)"
sessions_path="$(mktemp)"
trap 'rm -f "${bootstrap_path}" "${sessions_path}"' EXIT

echo "==> GET ${BASE_URL}/api/v2/bootstrap"
bootstrap_status="$(
  curl -sS \
    -o "${bootstrap_path}" \
    -w "%{http_code}" \
    -H "${auth_header}" \
    "${BASE_URL}/api/v2/bootstrap"
)"
if [[ "${bootstrap_status}" != "200" ]]; then
  echo "bootstrap failed (HTTP ${bootstrap_status})" >&2
  cat "${bootstrap_path}" >&2
  exit 1
fi

echo "==> GET ${BASE_URL}/api/v2/sessions"
sessions_status="$(
  curl -sS \
    -o "${sessions_path}" \
    -w "%{http_code}" \
    -H "${auth_header}" \
    "${BASE_URL}/api/v2/sessions"
)"
if [[ "${sessions_status}" != "200" ]]; then
  echo "sessions failed (HTTP ${sessions_status})" >&2
  cat "${sessions_path}" >&2
  exit 1
fi

host_version="$(
  sed -n 's/.*"hostVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${bootstrap_path}" | head -n1
)"
session_count="$(
  grep -o '"id"[[:space:]]*:' "${sessions_path}" | wc -l | tr -d ' '
)"

echo "bootstrap ok (hostVersion=${host_version:-unknown})"
echo "sessions ok (count=${session_count})"

if [[ "${RUN_REALTIME}" -eq 1 ]]; then
  echo "==> realtime probe"
  swift scripts/manual_realtime_probe.swift \
    --base-url "${BASE_URL}" \
    --token "${ONIBI_PAIRING_TOKEN}" \
    --subscribe \
    --request-buffer \
    --text "echo onibi-smoke" \
    --key enter \
    --listen-seconds 2
  echo "realtime probe finished"
else
  echo "realtime probe skipped"
fi

echo "smoke check passed"
