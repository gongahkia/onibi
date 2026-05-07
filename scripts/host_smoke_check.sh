#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s [--output DIR]\n' "$(basename "$0")"
  printf '\n'
  printf 'Runs a local Debian/Ubuntu host posture smoke check with temporary outputs by default.\n'
}

output_root=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      if [[ $# -lt 2 ]]; then
        printf 'error: --output requires a directory\n' >&2
        exit 2
      fi
      output_root="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'error: unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
if [[ -z "${output_root}" ]]; then
  output_root="$(mktemp -d "${TMPDIR:-/tmp}/piranesi-host-smoke.XXXXXX")"
else
  mkdir -p "${output_root}"
  output_root="$(cd -- "${output_root}" && pwd)"
fi

evidence_dir="${output_root}/evidence"
report_dir="${output_root}/report"
mkdir -p "${evidence_dir}" "${report_dir}"

cd "${repo_root}"

uv run piranesi doctor .
uv run piranesi collect --output "${evidence_dir}" --no-trivy
uv run piranesi assess "${evidence_dir}" \
  --output "${report_dir}" \
  --analysis deterministic \
  --format both

HOST_SMOKE_EVIDENCE="${evidence_dir}" HOST_SMOKE_REPORT="${report_dir}" python - <<'PY'
from __future__ import annotations

import json
import os
from pathlib import Path

evidence_dir = Path(os.environ["HOST_SMOKE_EVIDENCE"])
report_dir = Path(os.environ["HOST_SMOKE_REPORT"])
manifest_path = evidence_dir / "collection-manifest.json"
report_path = report_dir / "host-report.json"
markdown_path = report_dir / "host-report.md"

if not manifest_path.is_file():
    raise SystemExit(f"missing collection manifest: {manifest_path}")
if not report_path.is_file():
    raise SystemExit(f"missing JSON report: {report_path}")
if not markdown_path.is_file():
    raise SystemExit(f"missing Markdown report: {markdown_path}")

manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
report = json.loads(report_path.read_text(encoding="utf-8"))

commands = manifest.get("commands")
if not isinstance(commands, list) or not commands:
    raise SystemExit("collection-manifest.json does not contain command results")

host_metadata = report.get("host_metadata")
if not isinstance(host_metadata, dict) or not host_metadata.get("hostname"):
    raise SystemExit("host-report.json is missing host_metadata.hostname")

top_actions = report.get("top_actions")
if not isinstance(top_actions, list):
    raise SystemExit("host-report.json is missing top_actions")

collection_health = report.get("collection_health")
if not isinstance(collection_health, dict):
    raise SystemExit("host-report.json is missing collection_health")
status_counts = collection_health.get("status_counts")
if not isinstance(status_counts, dict):
    raise SystemExit("collection_health is missing status_counts")

snapshot = report.get("snapshot")
identity = snapshot.get("identity") if isinstance(snapshot, dict) else None
hostname = identity.get("hostname") if isinstance(identity, dict) else None
if not hostname:
    raise SystemExit("host-report.json is missing snapshot.identity.hostname")

summary = ", ".join(
    f"{key}={value}" for key, value in sorted(status_counts.items()) if value
) or "none"
print(f"collection health: {summary}")
PY

printf 'Host smoke check completed.\n'
printf 'Evidence: %s\n' "${evidence_dir}"
printf 'Report: %s\n' "${report_dir}"
