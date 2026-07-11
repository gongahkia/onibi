#!/usr/bin/env bash
set -euo pipefail

require_artifacts=0
smoke_dir=""

while (($#)); do
  case "$1" in
    --require-artifacts)
      require_artifacts=1
      shift
      ;;
    --smoke-dir)
      if (($# < 2)); then
        echo "--smoke-dir requires a path" >&2
        exit 1
      fi
      smoke_dir="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

doc="docs/fresh-machine-smoke.md"
refs=(README.md docs/getting-started.md docs/release.md)
screenshots=(
  docs/assets/fresh-machine/macos-install.png
  docs/assets/fresh-machine/macos-doctor-preflight.png
  docs/assets/fresh-machine/macos-up.png
  docs/assets/fresh-machine/macos-doctor-after-upgrade.png
  docs/assets/fresh-machine/macos-uninstall.png
  docs/assets/fresh-machine/ubuntu-install.png
  docs/assets/fresh-machine/ubuntu-doctor-preflight.png
  docs/assets/fresh-machine/ubuntu-up.png
  docs/assets/fresh-machine/ubuntu-doctor-after-upgrade.png
  docs/assets/fresh-machine/ubuntu-uninstall.png
)
transcripts=(
  machine.txt
  macos-install.txt
  macos-version.txt
  macos-status-initial.json
  macos-hooks-dry-run.txt
  macos-doctor-preflight.txt
  macos-up.txt
  macos-up.log
  macos-doctor-after-upgrade.txt
  macos-uninstall-dry-run.txt
  macos-uninstall.txt
  ubuntu-install.txt
  ubuntu-version.txt
  ubuntu-status-initial.json
  ubuntu-hooks-dry-run.txt
  ubuntu-doctor-preflight.txt
  ubuntu-up.txt
  ubuntu-up.log
  ubuntu-doctor-after-upgrade.txt
  ubuntu-uninstall-dry-run.txt
  ubuntu-uninstall.txt
)

die() {
  echo "$*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || die "missing $1"
}

require_text() {
  local needle="$1"
  local file="$2"
  grep -Fq "$needle" "$file" || die "$file missing: $needle"
}

require_regex() {
  local pattern="$1"
  local file="$2"
  grep -Eq "$pattern" "$file" || die "$file missing pattern: $pattern"
}

require_png() {
  local file="$1"
  require_file "$file"
  [[ -s "$file" ]] || die "empty screenshot: $file"
  local sig
  sig="$(od -An -tx1 -N8 "$file" | tr -d ' \n')"
  [[ "$sig" == "89504e470d0a1a0a" ]] || die "not a PNG: $file"
}

require_file "$doc"
for ref in "${refs[@]}"; do
  require_file "$ref"
  require_text "fresh-machine-smoke.md" "$ref"
done

require_text "Current verification status: real macOS and Ubuntu VM output/screenshots are not" "$doc"
require_text "## Artifacts To Capture" "$doc"
require_text "## macOS 14+" "$doc"
require_text "## Debian/Ubuntu 24.04" "$doc"
require_text "## Release Artifact Smoke" "$doc"
require_text "scripts/release-smoke.sh dist" "$doc"
require_text "## Close Criteria" "$doc"

for screenshot in "${screenshots[@]}"; do
  require_text "| \`$screenshot\` |" "$doc"
done

for os in macos ubuntu; do
  require_regex "onibi status --json --no-doctor --no-hooks --no-update >\"\\\$ONIBI_SMOKE_DIR/${os}-status-initial\\.json\"" "$doc"
  require_text "onibi install-hooks --dry-run 2>&1 | tee \"\$ONIBI_SMOKE_DIR/${os}-hooks-dry-run.txt\"" "$doc"
  require_text "onibi doctor --mode preflight --offline --color=never 2>&1 | tee \"\$ONIBI_SMOKE_DIR/${os}-doctor-preflight.txt\"" "$doc"
  require_text "onibi up --transport=lan --log-file \"\$ONIBI_SMOKE_DIR/${os}-up.log\" 2>&1 | tee \"\$ONIBI_SMOKE_DIR/${os}-up.txt\"" "$doc"
  require_text "onibi doctor --release --after-upgrade --offline --color=never 2>&1 | tee \"\$ONIBI_SMOKE_DIR/${os}-doctor-after-upgrade.txt\"" "$doc"
  require_text "onibi uninstall --dry-run --service --hooks --all-hooks --state 2>&1 | tee \"\$ONIBI_SMOKE_DIR/${os}-uninstall-dry-run.txt\"" "$doc"
  require_text "onibi uninstall --yes --service --hooks --all-hooks --state 2>&1 | tee \"\$ONIBI_SMOKE_DIR/${os}-uninstall.txt\"" "$doc"
done

require_text "brew install gongahkia/onibi/onibi 2>&1 | tee \"\$ONIBI_SMOKE_DIR/macos-install.txt\"" "$doc"
require_text "curl -fsSL https://get.onibi.sh | sh 2>&1 | tee \"\$ONIBI_SMOKE_DIR/ubuntu-install.txt\"" "$doc"
require_text "sudo apt install -y ca-certificates curl gnupg tar" "$doc"

if ((require_artifacts)); then
  if grep -Fq "Current verification status: real macOS and Ubuntu VM output/screenshots are not" "$doc" || grep -Fq "captured in this repo yet" "$doc"; then
    die "$doc still says real output/screenshots are not captured"
  fi
  for screenshot in "${screenshots[@]}"; do
    require_png "$screenshot"
  done
fi

if [[ -n "$smoke_dir" ]]; then
  [[ -d "$smoke_dir" ]] || die "missing smoke dir: $smoke_dir"
  for transcript in "${transcripts[@]}"; do
    [[ -s "$smoke_dir/$transcript" ]] || die "missing transcript: $smoke_dir/$transcript"
  done
fi

echo "fresh-machine doc check ok"
