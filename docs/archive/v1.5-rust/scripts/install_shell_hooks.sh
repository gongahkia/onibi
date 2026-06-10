#!/usr/bin/env bash
set -euo pipefail

START_MARKER="# >>> onibi >>>"
END_MARKER="# <<< onibi <<<"

shell="zsh"
remote_control=0
uninstall=0
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
log_path="$HOME/.config/onibi/terminal.log"
socket_path="$HOME/.config/onibi/control.sock"
proxy_version="dev"
proxy_binary=""

usage() {
  cat <<'USAGE'
usage: ./scripts/install_shell_hooks.sh [options]

Options:
  --shell zsh                 Target shell rc file (default: zsh)
  --remote-control            Include OnibiSessionProxy bootstrap block
  --proxy-binary PATH         Proxy binary path (default: <repo>/.build/debug/OnibiSessionProxy)
  --log-path PATH             Log file path (default: ~/.config/onibi/terminal.log)
  --socket-path PATH          Proxy socket path (default: ~/.config/onibi/control.sock)
  --repo-root PATH            Repo root used for default proxy path
  --uninstall                 Remove Onibi hook block only
  -h, --help                  Show this help
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --shell)
      shell="${2:-}"
      shift 2
      ;;
    --remote-control)
      remote_control=1
      shift
      ;;
    --proxy-binary)
      proxy_binary="${2:-}"
      shift 2
      ;;
    --log-path)
      log_path="${2:-}"
      shift 2
      ;;
    --socket-path)
      socket_path="${2:-}"
      shift 2
      ;;
    --repo-root)
      repo_root="${2:-}"
      shift 2
      ;;
    --uninstall)
      uninstall=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ "$shell" != "zsh" ]]; then
  echo "only zsh is supported by this script right now (received: $shell)" >&2
  exit 1
fi

rc_file="$HOME/.zshrc"

if [[ -z "$proxy_binary" ]]; then
  proxy_binary="$repo_root/.build/debug/OnibiSessionProxy"
fi

mkdir -p "$HOME/.config/onibi"
touch "$rc_file"

backup_path="$rc_file.onibi-backup.$(date +%Y%m%d%H%M%S)"
cp "$rc_file" "$backup_path"

tmp_file="$(mktemp)"
awk -v start="$START_MARKER" -v end="$END_MARKER" '
function trim(s) {
  sub(/^[[:space:]]+/, "", s)
  sub(/[[:space:]]+$/, "", s)
  return s
}
{
  t = trim($0)
  if (t == start) { inblock = 1; next }
  if (inblock == 1) {
    if (t == end) { inblock = 0 }
    next
  }
  print
}
' "$rc_file" > "$tmp_file"
mv "$tmp_file" "$rc_file"

if [[ "$uninstall" -eq 1 ]]; then
  if zsh -n "$rc_file"; then
    echo "Onibi shell hook block removed from $rc_file"
    echo "backup saved at $backup_path"
    exit 0
  fi
  cp "$backup_path" "$rc_file"
  echo "zsh parse check failed after uninstall; restored backup from $backup_path" >&2
  exit 1
fi

{
  cat <<EOF

$START_MARKER
# Onibi Shell Integration - DO NOT EDIT
EOF

  if [[ "$remote_control" -eq 1 ]]; then
    cat <<EOF
export ONIBI_PROXY_BINARY="$proxy_binary"
if [[ -o interactive ]] && [[ -z "\$ONIBI_SESSION_PROXY_ACTIVE" ]] && ([[ "\$TERM_PROGRAM" == "ghostty" ]] || [[ -n "\$GHOSTTY_RESOURCES_DIR" ]]) && [[ -x "\$ONIBI_PROXY_BINARY" ]]; then
    export ONIBI_SESSION_PROXY_ACTIVE=1
    export ONIBI_PROXY_SOCKET_PATH="$socket_path"
    export ONIBI_HOST_SESSION_ID="\${TERM_SESSION_ID:-\$(uuidgen 2>/dev/null || echo \$\$)}"
    export ONIBI_PARENT_SHELL="\${SHELL:-/bin/zsh}"
    export ONIBI_PARENT_SHELL_ARGS="-l"
    export ONIBI_PROXY_VERSION="$proxy_version"
    exec "\$ONIBI_PROXY_BINARY"
fi

EOF
  fi

  cat <<EOF
_onibi_preexec() {
    local session_id="\${TERM_SESSION_ID:-\$\$}"
    echo "\$(date -Iseconds)|CMD_START|\$session_id|\$1" >> "$log_path"
}

_onibi_precmd() {
    local exit_code=\$?
    local session_id="\${TERM_SESSION_ID:-\$\$}"
    echo "\$(date -Iseconds)|CMD_END|\$session_id|\$exit_code" >> "$log_path"
}

autoload -Uz add-zsh-hook
add-zsh-hook preexec _onibi_preexec
add-zsh-hook precmd _onibi_precmd
$END_MARKER
EOF
} >> "$rc_file"

if zsh -n "$rc_file"; then
  echo "Onibi shell hooks installed to $rc_file"
  echo "backup saved at $backup_path"
  if [[ "$remote_control" -eq 1 ]]; then
    echo "remote control bootstrap enabled (proxy: $proxy_binary)"
  else
    echo "remote control bootstrap disabled (logging-only hooks)"
  fi
  echo "next: run 'exec zsh -l' and open a new Ghostty tab/window."
  exit 0
fi

cp "$backup_path" "$rc_file"
echo "zsh parse check failed; restored backup from $backup_path" >&2
exit 1
