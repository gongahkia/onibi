#!/usr/bin/env sh
set -eu

repo_url="${KELP_PI_REPO_URL:-https://github.com/gongahkia/kelp.git}"
repo_ref="${KELP_PI_REF:-main}"
source_dir="${KELP_PI_SOURCE_DIR:-/opt/kelp/src}"
release_repo="${KELP_PI_RELEASE_REPO:-gongahkia/kelp}"
release_tag="${KELP_PI_RELEASE_TAG:-latest}"
release_asset="${KELP_PI_RELEASE_ASSET:-kelp-pi-agent-aarch64}"
agent_url="${KELP_PI_AGENT_URL:-}"
agent_sha256="${KELP_PI_AGENT_SHA256:-}"
device_id="${KELP_PI_DEVICE_ID:-$(hostname)}"
skip_apt="${KELP_PI_SKIP_APT:-0}"
skip_nuclei="${KELP_PI_SKIP_NUCLEI:-0}"
skip_start="${KELP_PI_SKIP_START:-0}"
allow_non_pi5="${KELP_PI_ALLOW_NON_PI5:-0}"
build_from_source="${KELP_PI_BUILD_FROM_SOURCE:-0}"
fallback_source="${KELP_PI_RELEASE_FALLBACK_SOURCE:-0}"
preflight_only="${KELP_PI_PREFLIGHT_ONLY:-0}"
current_step="startup"

usage() {
  cat <<'USAGE'
usage: install-kelp-pi.sh [options]

Options:
  --release-repo OWNER/REPO  GitHub release repo. Default: gongahkia/kelp
  --release-tag TAG          GitHub release tag. Default: latest
  --release-asset NAME       Release binary asset. Default: kelp-pi-agent-aarch64
  --agent-url URL            Custom prebuilt aarch64 kelp-pi-agent binary URL
  --agent-sha256 SHA256      Expected SHA-256 for --agent-url
  --build-from-source        Build kelp-pi-agent locally instead of using release binary
  --fallback-source          Build from source if release binary download fails
  --repo URL                 Git repo to clone for install scripts. Default: https://github.com/gongahkia/kelp.git
  --ref REF                  Git branch/tag to install scripts from. Default: main
  --source-dir DIR           Source checkout path. Default: /opt/kelp/src
  --device-id ID             Pi key label. Default: hostname
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
    --agent-url)
      [ $# -ge 2 ] || fail "--agent-url requires a value"
      agent_url="$2"
      shift 2
      ;;
    --agent-sha256)
      [ $# -ge 2 ] || fail "--agent-sha256 requires a value"
      agent_sha256="$2"
      shift 2
      ;;
    --build-from-source)
      build_from_source=1
      shift
      ;;
    --fallback-source)
      fallback_source=1
      shift
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
  packages="ca-certificates curl git unzip file jq nftables network-manager dnsmasq iproute2 iputils-ping dnsutils tcpdump nmap sudo"
  if [ "$build_from_source" = "1" ] || [ "$fallback_source" = "1" ]; then
    packages="$packages build-essential cargo pkg-config"
    check_disk "$(dirname "$source_dir")" 5242880
  fi
  apt-get install -y --no-install-recommends $packages
fi

need curl
need git
need install
need awk
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

agent_source="release"
if [ -n "$agent_url" ]; then
  agent_source="custom-url"
  [ -n "$agent_sha256" ] || fail "--agent-url requires --agent-sha256"
  log "downloading custom kelp-pi-agent binary"
  agent_build="$tmp/kelp-pi-agent"
  download "$agent_url" "$agent_build"
  actual="$(hash_file "$agent_build")"
  [ "$actual" = "$agent_sha256" ] || fail "agent sha256 mismatch: expected $agent_sha256 found $actual"
  chmod 0755 "$agent_build"
elif [ "$build_from_source" = "1" ]; then
  agent_source="source"
  log "building kelp-pi-agent from source"
  need cargo
  (cd "$pi_dir" && cargo build --release --locked)
  agent_build="$pi_dir/target/release/kelp-pi-agent"
  agent_sha256="$(hash_file "$agent_build")"
else
  log "downloading release kelp-pi-agent binary"
  agent_url="$(release_url "$release_asset")"
  checksum_url="$(release_url "$release_asset.sha256")"
  agent_build="$tmp/kelp-pi-agent"
  if ! download "$agent_url" "$agent_build" || ! download "$checksum_url" "$tmp/$release_asset.sha256"; then
    if [ "$fallback_source" = "1" ]; then
      agent_source="source"
      log "release binary unavailable; building from source"
      need cargo
      (cd "$pi_dir" && cargo build --release --locked)
      agent_build="$pi_dir/target/release/kelp-pi-agent"
      agent_sha256="$(hash_file "$agent_build")"
    else
      fail "release asset unavailable; publish $release_asset and $release_asset.sha256 or rerun with --build-from-source"
    fi
  else
    agent_sha256="$(awk '{ print $1 }' "$tmp/$release_asset.sha256")"
    actual="$(hash_file "$agent_build")"
    [ "$actual" = "$agent_sha256" ] || fail "agent sha256 mismatch: expected $agent_sha256 found $actual"
    chmod 0755 "$agent_build"
  fi
fi

"$agent_build" version >/dev/null || fail "downloaded agent binary does not run"

log "installing agent, systemd units, and helper commands"
KELP_PI_AGENT_BUILD="$agent_build" "$scripts_dir/install-agent-binary.sh" /
"$scripts_dir/install-systemd.sh" /
"$scripts_dir/install-field-tools.sh" /

if [ "$skip_nuclei" != "1" ]; then
  log "installing pinned Nuclei ARM64 binary"
  if ! "$scripts_dir/fetch-nuclei-arm64.sh" /; then
    fail "Nuclei install failed; rerun with --skip-nuclei to install agent only"
  fi
else
  log "skipping Nuclei install"
fi
"$scripts_dir/write-scanner-strategy.sh" / >/dev/null

log "creating users and data directories"
systemd-sysusers /usr/lib/sysusers.d/kelp-pi-agent.conf
systemd-tmpfiles --create /usr/lib/tmpfiles.d/kelp-pi-agent.conf

if [ ! -f /var/lib/kelp-pi/keys/pi-ed25519.key.json ]; then
  log "generating Pi identity key"
  runuser -u kelp-pi -- /usr/local/bin/kelp-pi-agent keygen \
    --key-dir /var/lib/kelp-pi/keys \
    --label "$device_id" >/dev/null
else
  log "keeping existing Pi identity key"
fi

log "writing install metadata"
install -d /etc/kelp-pi
installed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cat > /etc/kelp-pi/install.json <<META
{
  "installed_at": "$installed_at",
  "agent_source": "$agent_source",
  "agent_url": "$agent_url",
  "agent_sha256": "$agent_sha256",
  "release_repo": "$release_repo",
  "release_tag": "$release_tag",
  "release_asset": "$release_asset",
  "repo_url": "$repo_url",
  "repo_ref": "$repo_ref",
  "source_dir": "$source_dir"
}
META

log "starting service"
systemctl daemon-reload
if [ "$skip_start" != "1" ]; then
  if ! systemctl enable --now kelp-pi-agent.service; then
    journalctl -u kelp-pi-agent.service -n 80 --no-pager >&2 || true
    fail "service start failed"
  fi
fi

log "checking installation"
/usr/local/bin/kelp-pi status
if ! runuser -u kelp-pi -- /usr/local/bin/kelp-pi-agent doctor --data-dir /var/lib/kelp-pi >/dev/null; then
  journalctl -u kelp-pi-agent.service -n 80 --no-pager >&2 || true
  fail "doctor failed after install"
fi

cat <<'DONE'

Kelp Pi is installed.

Next:
  kelp-pi
  kelp-pi models install qwen2.5:0.5b
  kelp-pi doctor
  kelp-pi logs

Network/AP hardening is not auto-applied because it can disconnect headless SSH.
When ready:
  sudo kelp-pi network-render 'replace-with-a-long-wpa3-passphrase' --allow-outbound control.example.com:443
  sudo kelp-pi network-apply
  sudo reboot

Full hardware acceptance after network setup:
  sudo kelp-pi validate-node
DONE
