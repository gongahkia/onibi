#!/usr/bin/env sh
set -eu

agent_bin="${KELP_PI_AGENT_BIN:-/usr/local/bin/kelp-pi-agent}"
service="${KELP_PI_SERVICE:-kelp-pi-agent.service}"
data_dir="${KELP_PI_DATA_DIR:-/var/lib/kelp-pi}"
agent_user="${KELP_PI_USER:-kelp-pi}"
install_meta="${KELP_PI_INSTALL_META:-/etc/kelp-pi/install.json}"
release_repo="${KELP_PI_RELEASE_REPO:-gongahkia/kelp}"
release_tag="${KELP_PI_RELEASE_TAG:-latest}"
release_asset="${KELP_PI_RELEASE_ASSET:-kelp-pi-agent-aarch64}"
ollama_install_url="${KELP_PI_OLLAMA_INSTALL_URL:-https://ollama.com/install.sh}"

usage() {
  cat <<'USAGE'
usage: kelp-pi [command]

Commands:
  setup | wizard          Show first-run health, hardware, model, and next-step guide
  preflight              Show hardware, OS, network-manager, nftables, and throttle readiness
  status                 Show agent, service, and data-dir status
  version                Show helper, agent, and install metadata
  update                 Update kelp-pi-agent from GitHub release asset with rollback
  doctor                 Run agent doctor as kelp-pi
  selfcheck              Run agent selfcheck as kelp-pi
  models                 List RAM-gated Ollama model catalog
  models install [MODEL] Install Ollama if needed and pull a selectable model
  models local           List local Ollama models
  models remove MODEL    Remove a local Ollama model
  logs [N]               Show recent service logs
  start|stop|restart     Control kelp-pi-agent.service
  network-render PASS    Render AP/firewall hardening files
  network-apply [CONFIG] Apply AP/firewall hardening
  network-backup         Snapshot AP/firewall config and current nftables ruleset
  recover network        Restore latest AP/firewall snapshot; requires --force
  validate-node          Run node validation
  reset --force          Wipe data dir, recreate layout, regenerate identity key
  wipe-data --force      Wipe data dir only
  uninstall --force      Stop service and remove installed Kelp Pi files
  next                   Print next commands

Common flow:
  kelp-pi
  kelp-pi models install qwen2.5:0.5b
  kelp-pi doctor
USAGE
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    printf 'missing command: %s\n' "$1" >&2
    exit 69
  }
}

as_root() {
  if [ "$(id -u)" = "0" ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    printf 'run as root or install sudo: %s\n' "$*" >&2
    exit 77
  fi
}

as_agent() {
  if [ "$(id -u)" = "0" ]; then
    runuser -u "$agent_user" -- "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo -u "$agent_user" "$@"
  else
    printf 'run as root or install sudo: %s\n' "$*" >&2
    exit 77
  fi
}

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
  need curl
  url="$1"
  out="$2"
  curl -fL --connect-timeout 15 --retry 2 --retry-delay 2 "$url" -o "$out"
}

service_exists() {
  command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files "$service" >/dev/null 2>&1
}

status() {
  need "$agent_bin"
  printf 'agent: %s\n' "$("$agent_bin" version)"
  if command -v systemctl >/dev/null 2>&1; then
    active="$(systemctl is-active "$service" 2>/dev/null || true)"
    enabled="$(systemctl is-enabled "$service" 2>/dev/null || true)"
    printf 'service: %s enabled=%s active=%s\n' "$service" "$enabled" "$active"
  fi
  if [ -d "$data_dir" ]; then
    printf 'data: %s\n' "$data_dir"
  else
    printf 'data: missing %s\n' "$data_dir"
  fi
}

version_info() {
  printf 'helper: kelp-pi\n'
  if [ -x "$agent_bin" ]; then
    printf 'agent: %s\n' "$("$agent_bin" version)"
  else
    printf 'agent: missing %s\n' "$agent_bin"
  fi
  if [ -r "$install_meta" ]; then
    printf 'install: %s\n' "$install_meta"
    if command -v jq >/dev/null 2>&1; then
      jq -r '"source=\(.agent_source) release=\(.release_repo // "-")#\(.release_tag // "-") asset=\(.release_asset // "-") installed_at=\(.installed_at)"' "$install_meta"
    else
      sed -n '1,3p' "$install_meta"
    fi
  else
    printf 'install: missing %s\n' "$install_meta"
  fi
}

ram_kib() {
  if [ -r /proc/meminfo ]; then
    awk '/^MemTotal:/ { print $2 }' /proc/meminfo
  fi
}

ram_tier() {
  mem="$(ram_kib)"
  if [ -z "$mem" ]; then
    printf 'unknown'
    return
  fi
  if [ "$mem" -lt 3145728 ]; then
    printf 'unsupported (<3GiB detected)'
  elif [ "$mem" -lt 7340032 ]; then
    printf '4GB profile'
  elif [ "$mem" -lt 14680064 ]; then
    printf '8GB profile'
  else
    printf '16GB profile'
  fi
}

ram_line() {
  mem="$(ram_kib)"
  if [ -z "$mem" ]; then
    printf 'unknown'
  else
    printf '%s (%s KiB)' "$(ram_tier)" "$mem"
  fi
}

storage_line() {
  if [ -d "$data_dir" ]; then
    df -Pk "$data_dir" | awk 'NR==2 { printf "%d MiB free on %s", int($4 / 1024), $6 }'
  else
    df -Pk / | awk 'NR==2 { printf "%d MiB free on %s", int($4 / 1024), $6 }'
  fi
}

ok_line() {
  printf '%-22s %s\n' "$1:" "$2"
}

model_line() {
  if [ -r /proc/device-tree/model ]; then
    tr -d '\000' < /proc/device-tree/model
  else
    printf 'unknown'
  fi
}

os_line() {
  if [ -r /etc/os-release ]; then
    . /etc/os-release
    printf '%s %s' "${PRETTY_NAME:-unknown}" "${VERSION_CODENAME:-}"
  else
    printf 'unknown'
  fi
}

throttle_line() {
  if command -v vcgencmd >/dev/null 2>&1; then
    vcgencmd get_throttled 2>/dev/null | sed 's/^throttled=//' || printf 'unknown'
  else
    printf 'vcgencmd missing'
  fi
}

command_line() {
  if command -v "$1" >/dev/null 2>&1; then
    printf 'present'
  else
    printf 'missing'
  fi
}

preflight() {
  printf 'Kelp Pi preflight\n\n'
  ok_line model "$(model_line)"
  ok_line arch "$(uname -m 2>/dev/null || true)"
  ok_line os "$(os_line)"
  ok_line ram "$(ram_line)"
  ok_line storage "$(storage_line)"
  ok_line throttle "$(throttle_line)"
  ok_line nmcli "$(command_line nmcli)"
  if command -v systemctl >/dev/null 2>&1; then
    ok_line NetworkManager "$(systemctl is-active NetworkManager 2>/dev/null || true)"
  fi
  ok_line nft "$(command_line nft)"
  ok_line sshd "$(systemctl is-active ssh 2>/dev/null || systemctl is-active sshd 2>/dev/null || true)"
}

setup_wizard() {
  printf 'Kelp Pi setup\n\n'
  ok_line model "$(model_line)"
  if [ -x "$agent_bin" ]; then
    ok_line agent "$("$agent_bin" version)"
  else
    ok_line agent "missing $agent_bin"
  fi
  if command -v systemctl >/dev/null 2>&1; then
    ok_line service "$(systemctl is-active "$service" 2>/dev/null || true)"
  fi
  ok_line ram "$(ram_line)"
  ok_line storage "$(storage_line)"
  if [ -x /opt/kelp-pi/bin/nuclei ]; then
    ok_line nuclei "/opt/kelp-pi/bin/nuclei"
  else
    ok_line nuclei "missing"
  fi
  if [ -r /etc/kelp-pi/network-hardening.json ]; then
    ok_line network-hardening "rendered"
  else
    ok_line network-hardening "not rendered/applied"
  fi
  if command -v ollama >/dev/null 2>&1; then
    ollama_active="$(systemctl is-active ollama 2>/dev/null || true)"
    ok_line ollama "installed service=$ollama_active"
  else
    ok_line ollama "not installed"
  fi
  printf '\nSelectable models:\n'
  if [ -x "$agent_bin" ]; then
    "$agent_bin" ollama models || true
  else
    printf 'agent missing; cannot list model catalog\n'
  fi
  printf '\n'
  next_steps
}

next_steps() {
  cat <<'NEXT'
Next:
  kelp-pi models install qwen2.5:0.5b
  kelp-pi doctor
  sudo kelp-pi network-render 'change-this-long-wpa3-passphrase' --allow-outbound host:443
  sudo kelp-pi network-apply
  sudo kelp-pi validate-node
  kelp-pi logs
NEXT
}

update_agent() {
  agent_url=""
  agent_sha256=""
  no_restart=0

  while [ $# -gt 0 ]; do
    case "$1" in
      --agent-url)
        [ $# -ge 2 ] || { printf '--agent-url requires a value\n' >&2; exit 64; }
        agent_url="$2"
        shift 2
        ;;
      --agent-sha256)
        [ $# -ge 2 ] || { printf '--agent-sha256 requires a value\n' >&2; exit 64; }
        agent_sha256="$2"
        shift 2
        ;;
      --release-repo)
        [ $# -ge 2 ] || { printf '--release-repo requires a value\n' >&2; exit 64; }
        release_repo="$2"
        shift 2
        ;;
      --release-tag)
        [ $# -ge 2 ] || { printf '--release-tag requires a value\n' >&2; exit 64; }
        release_tag="$2"
        shift 2
        ;;
      --release-asset)
        [ $# -ge 2 ] || { printf '--release-asset requires a value\n' >&2; exit 64; }
        release_asset="$2"
        shift 2
        ;;
      --no-restart)
        no_restart=1
        shift
        ;;
      *)
        printf 'unknown update argument: %s\n' "$1" >&2
        exit 64
        ;;
    esac
  done

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT INT TERM
  new_bin="$tmp/$release_asset"

  if [ -z "$agent_url" ]; then
    agent_url="$(release_url "$release_asset")"
    checksum_url="$(release_url "$release_asset.sha256")"
    printf 'downloading %s\n' "$agent_url"
    download "$agent_url" "$new_bin"
    download "$checksum_url" "$tmp/$release_asset.sha256"
    agent_sha256="$(awk '{ print $1 }' "$tmp/$release_asset.sha256")"
  else
    printf 'downloading %s\n' "$agent_url"
    download "$agent_url" "$new_bin"
    [ -n "$agent_sha256" ] || {
      printf 'custom --agent-url requires --agent-sha256\n' >&2
      exit 65
    }
  fi

  actual="$(hash_file "$new_bin")"
  [ "$actual" = "$agent_sha256" ] || {
    printf 'sha256 mismatch: expected %s found %s\n' "$agent_sha256" "$actual" >&2
    exit 65
  }
  chmod 0755 "$new_bin"
  "$new_bin" version >/dev/null

  backup="$agent_bin.backup.$(date -u +%Y%m%dT%H%M%SZ)"
  if [ -x "$agent_bin" ]; then
    as_root cp -p "$agent_bin" "$backup"
  else
    backup=""
  fi
  as_root install -m 0755 "$new_bin" "$agent_bin"

  if [ "$no_restart" = "1" ]; then
    printf 'updated %s without restart\n' "$agent_bin"
    return
  fi

  if service_exists; then
    if ! as_root systemctl restart "$service"; then
      rollback "$backup"
      exit 70
    fi
    sleep 2
    if ! systemctl is-active --quiet "$service"; then
      printf 'service health failed after update\n' >&2
      as_root journalctl -u "$service" -n 80 --no-pager >&2 || true
      rollback "$backup"
      exit 70
    fi
  fi

  if ! as_agent "$agent_bin" doctor --data-dir "$data_dir" >/dev/null; then
    printf 'doctor failed after update\n' >&2
    rollback "$backup"
    exit 70
  fi

  printf 'updated %s\n' "$("$agent_bin" version)"
}

rollback() {
  backup="$1"
  [ -n "$backup" ] && [ -x "$backup" ] || {
    printf 'rollback unavailable; no backup binary\n' >&2
    return
  }
  printf 'rolling back to %s\n' "$backup" >&2
  as_root install -m 0755 "$backup" "$agent_bin"
  as_root systemctl restart "$service" || true
}

network_paths() {
  cat <<'PATHS'
/etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection
/etc/dnsmasq.d/kelp-pi-captive.conf
/etc/nftables.d/kelp-pi.nft
/etc/sysctl.d/90-kelp-pi-network.conf
/etc/kelp-pi/network-hardening.json
/boot/firmware/config.txt.kelp-pi-fragment
PATHS
}

network_backup() {
  reason="${1:-manual}"
  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  backup_dir="$data_dir/recovery/network-$timestamp"
  manifest_tmp="$(mktemp)"
  rules_tmp="$(mktemp)"
  trap 'rm -f "$manifest_tmp" "$rules_tmp"' EXIT INT TERM
  as_root mkdir -p "$backup_dir/files"
  printf 'reason\t%s\n' "$reason" >"$manifest_tmp"
  network_paths | while IFS= read -r path; do
    rel="${path#/}"
    if [ -e "$path" ]; then
      as_root mkdir -p "$backup_dir/files/$(dirname "$rel")"
      as_root cp -p "$path" "$backup_dir/files/$rel"
      printf 'present\t%s\t%s\n' "$path" "$rel" >>"$manifest_tmp"
    else
      printf 'missing\t%s\t%s\n' "$path" "$rel" >>"$manifest_tmp"
    fi
  done
  as_root install -m 0644 "$manifest_tmp" "$backup_dir/manifest.tsv"
  if command -v nft >/dev/null 2>&1 && as_root nft list ruleset >"$rules_tmp" 2>/dev/null; then
    as_root install -m 0644 "$rules_tmp" "$backup_dir/nft-ruleset.nft"
  fi
  printf 'network backup: %s\n' "$backup_dir"
}

latest_network_backup() {
  find "$data_dir/recovery" -maxdepth 1 -type d -name 'network-*' 2>/dev/null | sort | tail -n 1
}

recover_network() {
  backup="latest"
  force=0
  no_restart=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --backup)
        [ $# -ge 2 ] || { printf '--backup requires a value\n' >&2; exit 64; }
        backup="$2"
        shift 2
        ;;
      --force)
        force=1
        shift
        ;;
      --no-restart)
        no_restart=1
        shift
        ;;
      *)
        printf 'unknown recover network argument: %s\n' "$1" >&2
        exit 64
        ;;
    esac
  done
  [ "$force" = "1" ] || {
    printf 'recover network requires --force\n' >&2
    exit 64
  }
  if [ "$backup" = "latest" ]; then
    backup="$(latest_network_backup)"
  fi
  [ -n "$backup" ] && [ -d "$backup" ] || {
    printf 'network backup not found\n' >&2
    exit 66
  }
  manifest="$backup/manifest.tsv"
  [ -f "$manifest" ] || {
    printf 'network backup manifest missing: %s\n' "$manifest" >&2
    exit 66
  }
  while IFS='	' read -r state path rel; do
    case "$state" in
      reason|"")
        ;;
      present)
        as_root mkdir -p "$(dirname "$path")"
        as_root cp -p "$backup/files/$rel" "$path"
        ;;
      missing)
        as_root rm -f "$path"
        ;;
    esac
  done <"$manifest"
  if [ -s "$backup/nft-ruleset.nft" ] && command -v nft >/dev/null 2>&1; then
    as_root nft -f "$backup/nft-ruleset.nft" || true
  fi
  if [ "$no_restart" != "1" ] && command -v systemctl >/dev/null 2>&1; then
    as_root systemctl restart nftables 2>/dev/null || true
    as_root systemctl restart dnsmasq 2>/dev/null || true
    as_root systemctl restart NetworkManager 2>/dev/null || true
  fi
  printf 'recovered network from %s\n' "$backup"
}

ensure_ollama() {
  if command -v ollama >/dev/null 2>&1; then
    return
  fi
  need curl
  printf 'installing Ollama from %s\n' "$ollama_install_url"
  as_root sh -c "curl -fsSL '$ollama_install_url' | sh"
}

models_command() {
  sub="${1:-list}"
  [ $# -eq 0 ] || shift
  case "$sub" in
    list|catalog)
      need "$agent_bin"
      "$agent_bin" ollama models "$@"
      ;;
    install)
      model="${1:-qwen2.5:0.5b}"
      need "$agent_bin"
      "$agent_bin" ollama check --enable-ollama --model "$model"
      ensure_ollama
      as_root systemctl enable --now ollama >/dev/null 2>&1 || true
      ollama pull "$model"
      printf 'installed model: %s\n' "$model"
      ;;
    local|installed)
      ensure_ollama
      ollama list
      ;;
    remove|rm)
      [ $# -ge 1 ] || { printf 'usage: kelp-pi models remove MODEL\n' >&2; exit 64; }
      ensure_ollama
      ollama rm "$1"
      ;;
    *)
      printf 'unknown models command: %s\n' "$sub" >&2
      exit 64
      ;;
  esac
}

require_force() {
  [ "${1:-}" = "--force" ] || {
    printf '%s requires --force\n' "$2" >&2
    exit 64
  }
}

reset_data() {
  require_force "${1:-}" "reset"
  as_root systemctl stop "$service" || true
  as_agent "$agent_bin" wipe --force --data-dir "$data_dir"
  as_root systemd-tmpfiles --create /usr/lib/tmpfiles.d/kelp-pi-agent.conf
  as_agent "$agent_bin" keygen --key-dir "$data_dir/keys" --label "$(hostname)" >/dev/null
  as_root systemctl start "$service" || true
  printf 'reset complete\n'
}

wipe_data() {
  require_force "${1:-}" "wipe-data"
  as_agent "$agent_bin" wipe --force --data-dir "$data_dir"
}

uninstall() {
  keep_data=0
  force=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --force)
        force=1
        shift
        ;;
      --keep-data)
        keep_data=1
        shift
        ;;
      *)
        printf 'unknown uninstall argument: %s\n' "$1" >&2
        exit 64
        ;;
    esac
  done
  [ "$force" = "1" ] || {
    printf 'uninstall requires --force\n' >&2
    exit 64
  }
  as_root systemctl disable --now "$service" || true
  as_root rm -f \
    /etc/systemd/system/kelp-pi-agent.service \
    /usr/lib/sysusers.d/kelp-pi-agent.conf \
    /usr/lib/tmpfiles.d/kelp-pi-agent.conf \
    /usr/local/bin/kelp-pi-agent \
    /usr/local/bin/kelp-pi \
    /usr/local/sbin/kelp-pi-validate-node \
    /usr/local/sbin/kelp-pi-validate-scanner-sandbox \
    /usr/local/sbin/kelp-pi-validate-allow-outbound-reload \
    /usr/local/sbin/kelp-pi-validate-dns-egress \
    /usr/local/sbin/kelp-pi-validate-ap-isolation \
    /usr/local/sbin/kelp-pi-validate-field-acceptance \
    /usr/local/sbin/kelp-pi-validate-ollama-load \
    /usr/local/sbin/kelp-pi-validate-readonly-root \
    /usr/local/sbin/kelp-pi-validate-nuclei-scan
  as_root rm -rf /opt/kelp-pi
  if [ "$keep_data" != "1" ]; then
    as_root rm -rf /etc/kelp-pi "$data_dir"
  fi
  as_root systemctl daemon-reload || true
  printf 'uninstalled Kelp Pi\n'
}

if [ $# -eq 0 ]; then
  setup_wizard
  exit 0
fi

command="$1"
shift

case "$command" in
  help|--help|-h)
    usage
    ;;
  setup|wizard)
    setup_wizard
    ;;
  preflight)
    preflight
    ;;
  status)
    status
    ;;
  version|--version|-V)
    version_info
    ;;
  update)
    update_agent "$@"
    ;;
  doctor)
    need "$agent_bin"
    as_agent "$agent_bin" doctor --data-dir "$data_dir" "$@"
    ;;
  selfcheck)
    need "$agent_bin"
    as_agent "$agent_bin" selfcheck --data-dir "$data_dir" "$@"
    ;;
  models)
    models_command "$@"
    ;;
  logs)
    as_root journalctl -u "$service" -n "${1:-80}" --no-pager
    ;;
  start|stop|restart)
    as_root systemctl "$command" "$service"
    ;;
  validate-node)
    as_root kelp-pi-validate-node "$@"
    ;;
  network-backup)
    network_backup manual
    ;;
  network-render)
    [ $# -ge 1 ] || {
      printf 'usage: kelp-pi network-render WPA3_PASSPHRASE [--allow-outbound HOST:PORT...]\n' >&2
      exit 64
    }
    passphrase="$1"
    shift
    network_backup before-network-render >/dev/null
    as_root "$agent_bin" hardening render-network --output / --wpa3-passphrase "$passphrase" "$@"
    ;;
  network-apply)
    config="${1:-/etc/kelp-pi/network-hardening.json}"
    network_backup before-network-apply >/dev/null
    as_root "$agent_bin" hardening apply-network --config "$config"
    ;;
  recover)
    sub="${1:-}"
    [ $# -eq 0 ] || shift
    case "$sub" in
      network)
        recover_network "$@"
        ;;
      *)
        printf 'usage: kelp-pi recover network --force [--backup DIR|latest] [--no-restart]\n' >&2
        exit 64
        ;;
    esac
    ;;
  reset)
    reset_data "$@"
    ;;
  wipe-data)
    wipe_data "$@"
    ;;
  uninstall)
    uninstall "$@"
    ;;
  next)
    next_steps
    ;;
  *)
    printf 'unknown command: %s\n' "$command" >&2
    usage >&2
    exit 64
    ;;
esac
