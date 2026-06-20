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
  awk '/^MemTotal:/ { print $2 }' /proc/meminfo 2>/dev/null || printf '0\n'
}

ram_tier() {
  mem="$(ram_kib)"
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

storage_line() {
  if [ -d "$data_dir" ]; then
    df -h "$data_dir" | awk 'NR==2 { printf "%s free on %s", $4, $6 }'
  else
    df -h / | awk 'NR==2 { printf "%s free on %s", $4, $6 }'
  fi
}

ok_line() {
  printf '%-22s %s\n' "$1:" "$2"
}

setup_wizard() {
  printf 'Kelp Pi setup\n\n'
  if [ -x "$agent_bin" ]; then
    ok_line agent "$("$agent_bin" version)"
  else
    ok_line agent "missing $agent_bin"
  fi
  if command -v systemctl >/dev/null 2>&1; then
    ok_line service "$(systemctl is-active "$service" 2>/dev/null || true)"
  fi
  ok_line ram "$(ram_tier) ($(ram_kib) KiB)"
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
  network-render)
    [ $# -ge 1 ] || {
      printf 'usage: kelp-pi network-render WPA3_PASSPHRASE [--allow-outbound HOST:PORT...]\n' >&2
      exit 64
    }
    passphrase="$1"
    shift
    as_root "$agent_bin" hardening render-network --output / --wpa3-passphrase "$passphrase" "$@"
    ;;
  network-apply)
    config="${1:-/etc/kelp-pi/network-hardening.json}"
    as_root "$agent_bin" hardening apply-network --config "$config"
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
