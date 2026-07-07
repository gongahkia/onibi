#!/usr/bin/env sh
set -eu

repo_url="${KELP_PI_REPO_URL:-https://github.com/gongahkia/kelp.git}"
repo_ref="${KELP_PI_REF:-main}"
source_dir="${KELP_PI_SOURCE_DIR:-/opt/kelp/src}"
release_repo="${KELP_PI_RELEASE_REPO:-gongahkia/kelp}"
release_tag="${KELP_PI_RELEASE_TAG:-latest}"
release_asset="${KELP_PI_RELEASE_ASSET:-kelp-pi-aarch64}"
package_url="${KELP_PI_PACKAGE_URL:-${KELP_PI_AGENT_URL:-}}"
package_sha256="${KELP_PI_PACKAGE_SHA256:-${KELP_PI_AGENT_SHA256:-}}"
device_id="${KELP_PI_DEVICE_ID:-$(hostname)}"
skip_apt="${KELP_PI_SKIP_APT:-0}"
skip_nuclei="${KELP_PI_SKIP_NUCLEI:-0}"
skip_start="${KELP_PI_SKIP_START:-0}"
allow_non_pi5="${KELP_PI_ALLOW_NON_PI5:-0}"
preflight_only="${KELP_PI_PREFLIGHT_ONLY:-0}"
install_root="${KELP_PI_INSTALL_ROOT:-/opt/kelp-pi}"
data_dir="${KELP_PI_DATA_DIR:-/var/lib/kelp-pi}"
model_id="${KELP_PI_MODEL_ID:-qwen3-0.6b-q4_k_m}"
current_step="startup"

usage() {
  cat <<'USAGE'
usage: install-kelp-pi.sh [options]

Options:
  --release-repo OWNER/REPO  GitHub release repo. Default: gongahkia/kelp
  --release-tag TAG          GitHub release tag. Default: latest
  --release-asset NAME       Release package asset. Default: kelp-pi-aarch64
  --package-url URL          Custom prebuilt aarch64 kelp-pi package URL
  --package-sha256 SHA256    Expected SHA-256 for --package-url
  --repo URL                 Git repo to clone for install scripts. Default: https://github.com/gongahkia/kelp.git
  --ref REF                  Git branch/tag to install scripts from. Default: main
  --source-dir DIR           Source checkout path. Default: /opt/kelp/src
  --device-id ID             Pi key label. Default: hostname
  --model-id ID              Model manifest ID to fetch and verify. Default: qwen3-0.6b-q4_k_m
  --skip-apt                 Do not install apt packages
  --skip-nuclei              Do not download pinned Nuclei
  --skip-start               Install files but do not start systemd service
  --allow-non-pi5            Bypass Raspberry Pi 5 host check
  --preflight-only           Check host, disk, RAM, network, and exit before install
USAGE
}

log() {
  current_step="$*"
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR %s\n' "$*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"
}

on_exit() {
  code="$?"
  if [ "$code" -ne 0 ]; then
    printf 'ERROR failed during: %s\n' "$current_step" >&2
  fi
}
trap on_exit EXIT

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

release_url() {
  asset="$1"
  if [ "$release_tag" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download/%s\n' "$release_repo" "$asset"
  else
    printf 'https://github.com/%s/releases/download/%s/%s\n' "$release_repo" "$release_tag" "$asset"
  fi
}

download() {
  url="$1"
  out="$2"
  curl -fL --connect-timeout 15 --retry 2 --retry-delay 2 "$url" -o "$out"
}

mem_kib() {
  awk '/^MemTotal:/ { print $2 }' /proc/meminfo 2>/dev/null || printf '0\n'
}

check_min_ram() {
  mem="$(mem_kib)"
  [ "$mem" -ge 3145728 ] || fail "detected RAM ${mem}KiB; Kelp Pi requires at least 3GiB detected RAM for the 4GB profile"
}

check_disk() {
  path="$1"
  required_kib="$2"
  [ -d "$path" ] || path="/"
  avail="$(df -Pk "$path" | awk 'NR==2 { print $4 }')"
  [ "$avail" -ge "$required_kib" ] || fail "free disk at $path is ${avail}KiB; need ${required_kib}KiB"
}

warn() {
  printf 'WARN %s\n' "$*" >&2
}

host_model() {
  if [ -r /proc/device-tree/model ]; then
    tr -d '\000' < /proc/device-tree/model
  fi
}

check_pi_host() {
  if [ "$allow_non_pi5" = "1" ]; then
    return
  fi
  [ "$(uname -m)" = "aarch64" ] || fail "expected aarch64 Raspberry Pi OS, got $(uname -m)"
  model="$(host_model)"
  [ -n "$model" ] || fail "missing /proc/device-tree/model"
  case "$model" in
    *"Raspberry Pi 5"*) log "host: $model" ;;
    *) fail "expected Raspberry Pi 5, got: $model" ;;
  esac
}

check_throttle() {
  if command -v vcgencmd >/dev/null 2>&1; then
    throttled="$(vcgencmd get_throttled 2>/dev/null | sed 's/^throttled=//' || true)"
    case "$throttled" in
      ""|"0x0") log "power/thermal throttle flags: ${throttled:-none}" ;;
      *) warn "vcgencmd get_throttled=$throttled; check PSU/cooling before field acceptance" ;;
    esac
  else
    warn "vcgencmd missing; cannot inspect Pi power/thermal throttle flags"
  fi
}

safe_source_dir() {
  case "$source_dir" in
    ""|"/"|"/opt"|"/usr"|"/usr/local"|"/var"|"/var/lib")
      fail "unsafe --source-dir: $source_dir"
      ;;
  esac
}

wait_for_apt() {
  if command -v pgrep >/dev/null 2>&1; then
    tries=0
    while pgrep -x apt >/dev/null 2>&1 || pgrep -x apt-get >/dev/null 2>&1 || pgrep -x dpkg >/dev/null 2>&1; do
      tries=$((tries + 1))
      [ "$tries" -le 60 ] || fail "apt/dpkg lock still busy after 120s"
      printf 'waiting for apt/dpkg lock...\n'
      sleep 2
    done
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --release-repo)
      [ $# -ge 2 ] || fail "--release-repo requires a value"
      release_repo="$2"
      shift 2
      ;;
    --release-tag)
      [ $# -ge 2 ] || fail "--release-tag requires a value"
      release_tag="$2"
      shift 2
      ;;
    --release-asset)
      [ $# -ge 2 ] || fail "--release-asset requires a value"
      release_asset="$2"
      shift 2
      ;;
    --package-url|--agent-url)
      [ $# -ge 2 ] || fail "$1 requires a value"
      package_url="$2"
      shift 2
      ;;
    --package-sha256|--agent-sha256)
      [ $# -ge 2 ] || fail "$1 requires a value"
      package_sha256="$2"
      shift 2
      ;;
    --repo)
      [ $# -ge 2 ] || fail "--repo requires a value"
      repo_url="$2"
      shift 2
      ;;
    --ref)
      [ $# -ge 2 ] || fail "--ref requires a value"
      repo_ref="$2"
      shift 2
      ;;
    --source-dir)
      [ $# -ge 2 ] || fail "--source-dir requires a value"
      source_dir="$2"
      shift 2
      ;;
    --device-id)
      [ $# -ge 2 ] || fail "--device-id requires a value"
      device_id="$2"
      shift 2
      ;;
    --model-id)
      [ $# -ge 2 ] || fail "--model-id requires a value"
      model_id="$2"
      shift 2
      ;;
    --skip-apt)
      skip_apt=1
      shift
      ;;
    --skip-nuclei)
      skip_nuclei=1
      shift
      ;;
    --skip-start)
      skip_start=1
      shift
      ;;
    --allow-non-pi5)
      allow_non_pi5=1
      shift
      ;;
    --preflight-only)
      preflight_only=1
      shift
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

safe_source_dir
check_min_ram
check_disk "$(dirname "$source_dir")" 2097152
check_pi_host
check_throttle

log "checking network"
curl -fsI --connect-timeout 10 https://github.com >/dev/null || fail "cannot reach github.com; connect network before installing"

if [ "$preflight_only" = "1" ]; then
  printf 'Kelp Pi install preflight OK\n'
  exit 0
fi

[ "$(id -u)" = "0" ] || fail "run as root: curl -fsSL ... | sudo sh"

if [ "$skip_apt" != "1" ]; then
  log "installing OS packages"
  wait_for_apt
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  packages="ca-certificates curl git tar unzip file jq nftables network-manager dnsmasq iproute2 iputils-ping dnsutils tcpdump nmap sudo"
  apt-get install -y --no-install-recommends $packages
fi

need curl
need git
need install
need awk
need tar
need file
need grep
need runuser
need systemctl
need systemd-sysusers
need systemd-tmpfiles

tmp="$(mktemp -d)"
cleanup() {
  code="$?"
  rm -rf "$tmp"
  if [ "$code" -ne 0 ]; then
    printf 'ERROR failed during: %s\n' "$current_step" >&2
  fi
  exit "$code"
}
trap cleanup EXIT INT TERM

log "fetching Kelp install scripts: $repo_url#$repo_ref"
install -d "$(dirname "$source_dir")"
rm -rf "$source_dir.tmp"
git clone --depth 1 --branch "$repo_ref" "$repo_url" "$source_dir.tmp"
rm -rf "$source_dir"
mv "$source_dir.tmp" "$source_dir"

pi_dir="$source_dir/packages/pi-agent"
scripts_dir="$pi_dir/scripts"
[ -x "$scripts_dir/install-systemd.sh" ] || fail "source checkout missing Pi scripts"

package_source="release"
package_archive="$tmp/$release_asset"
if [ -n "$package_url" ]; then
  package_source="custom-url"
  [ -n "$package_sha256" ] || fail "--package-url requires --package-sha256"
  log "downloading custom kelp-pi package"
  download "$package_url" "$package_archive"
else
  log "downloading release kelp-pi package"
  package_url="$(release_url "$release_asset")"
  checksum_url="$(release_url "$release_asset.sha256")"
  download "$package_url" "$package_archive" || fail "release asset unavailable: $package_url"
  download "$checksum_url" "$tmp/$release_asset.sha256" || fail "release checksum unavailable: $checksum_url"
  package_sha256="$(awk '{ print $1 }' "$tmp/$release_asset.sha256")"
fi

actual="$(hash_file "$package_archive")"
[ "$actual" = "$package_sha256" ] || fail "package sha256 mismatch: expected $package_sha256 found $actual"

log "extracting kelp-pi package"
package_stage="$tmp/package"
install -d "$package_stage"
tar -xzf "$package_archive" -C "$package_stage"
[ -x "$package_stage/bin/kelp-pi" ] || fail "package missing bin/kelp-pi"
[ -x "$package_stage/run-kelp-pi.sh" ] || fail "package missing run-kelp-pi.sh"
[ -s "$package_stage/package-manifest.json" ] || fail "package missing package-manifest.json"
[ -s "$package_stage/policies/appsec-agent-baseline.toml" ] || fail "package missing policy pack"
[ -s "$package_stage/models/manifest.toml" ] || fail "package missing model manifest"
file "$package_stage/bin/kelp-pi" | grep -Eq 'ELF 64-bit.*(ARM aarch64|aarch64)' || fail "kelp-pi binary is not ELF aarch64"

log "installing kelp-pi runtime and systemd unit"
install -d "$(dirname "$install_root")" /usr/local/bin
rm -rf "$install_root"
mv "$package_stage" "$install_root"
ln -sfn "$install_root/run-kelp-pi.sh" /usr/local/bin/kelp-pi
"$scripts_dir/install-systemd.sh" /

"$install_root/run-kelp-pi.sh" version >/dev/null || fail "installed kelp-pi binary does not run"

log "creating users and data directories"
systemd-sysusers /usr/lib/sysusers.d/kelp-pi-agent.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/kelp-pi-agent.conf
install -d -m 0750 -o kelp-pi -g kelp-pi "$data_dir/policies" "$data_dir/models"
install -m 0644 -o kelp-pi -g kelp-pi "$install_root/policies/appsec-agent-baseline.toml" "$data_dir/policies/appsec-agent-baseline.toml"
install -m 0644 -o kelp-pi -g kelp-pi "$install_root/models/manifest.toml" "$data_dir/models/manifest.toml"

as_kelp_pi() {
  runuser -u kelp-pi -- env LD_LIBRARY_PATH="$install_root/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" \
    sh -c 'cd "$1" && shift && exec "$@"' sh "$data_dir" "$install_root/run-kelp-pi.sh" "$@"
}

if [ "$skip_nuclei" != "1" ]; then
  log "installing pinned Nuclei ARM64 binary"
  if ! "$scripts_dir/fetch-nuclei-arm64.sh" /; then
    fail "Nuclei install failed; rerun with --skip-nuclei to install runtime only"
  fi
else
  log "skipping Nuclei install"
fi
"$scripts_dir/write-scanner-strategy.sh" / >/dev/null

if [ ! -f /var/lib/kelp-pi/keys/pi-ed25519.key.json ]; then
  log "generating Pi identity key"
  as_kelp_pi keygen --data-dir "$data_dir" --label "$device_id" >/dev/null
else
  log "keeping existing Pi identity key"
fi

log "fetching and verifying model $model_id"
as_kelp_pi model fetch --data-dir "$data_dir" --manifest models/manifest.toml --id "$model_id" >/dev/null
as_kelp_pi model verify --data-dir "$data_dir" --manifest models/manifest.toml --id "$model_id" >/dev/null

log "writing install metadata"
install -d /etc/kelp-pi
installed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cat > /etc/kelp-pi/install.json <<META
{
  "installed_at": "$installed_at",
  "package_source": "$package_source",
  "package_url": "$package_url",
  "package_sha256": "$package_sha256",
  "release_repo": "$release_repo",
  "release_tag": "$release_tag",
  "release_asset": "$release_asset",
  "repo_url": "$repo_url",
  "repo_ref": "$repo_ref",
  "source_dir": "$source_dir",
  "install_root": "$install_root",
  "data_dir": "$data_dir",
  "model_id": "$model_id"
}
META

log "starting service"
systemctl daemon-reload
if [ "$skip_start" != "1" ]; then
  if ! systemctl enable --now kelp-pi.service; then
    journalctl -u kelp-pi.service -n 80 --no-pager >&2 || true
    fail "service start failed"
  fi
fi

log "checking installation"
if ! as_kelp_pi doctor --data-dir "$data_dir" --policy policies/appsec-agent-baseline.toml --models models/manifest.toml >/dev/null; then
  journalctl -u kelp-pi.service -n 80 --no-pager >&2 || true
  fail "doctor failed after install"
fi

cat <<'DONE'

Kelp Pi is installed.

Next:
  kelp-pi doctor --data-dir /var/lib/kelp-pi --policy /var/lib/kelp-pi/policies/appsec-agent-baseline.toml --models /var/lib/kelp-pi/models/manifest.toml
  kelp-pi model warm --data-dir /var/lib/kelp-pi --manifest /var/lib/kelp-pi/models/manifest.toml --id qwen3-0.6b-q4_k_m
  kelp-pi chat --data-dir /var/lib/kelp-pi

Network/AP hardening is not auto-applied because it can disconnect headless SSH.
Review docs/pi-recovery.md, then use scripts/apply-kelp-pi-network-hardening.sh.
DONE
