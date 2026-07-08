#!/usr/bin/env bash
set -euo pipefail

strict=false
out_dir="${ONIBI_TERMINAL_SMOKE_OUT:-}"
surface="${ONIBI_TERMINAL_SMOKE_SURFACE:-iPhone Safari paired Onibi cockpit}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/terminal-smoke.sh [--strict] [--out <dir>] [--surface <name>]

Runs host-side terminal fidelity checks for the Onibi phone cockpit matrix.
Default behavior treats missing optional tools as SKIP. --strict treats SKIP as FAIL.

Environment:
  ONIBI_TERMINAL_SMOKE_OUT=/path/to/artifacts
  ONIBI_TERMINAL_SMOKE_SURFACE="iPhone Safari paired Onibi cockpit"
EOF
}

while (($#)); do
  case "$1" in
    --strict) strict=true; shift ;;
    --out) out_dir="${2:-}"; shift 2 ;;
    --surface) surface="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

tmp="$(mktemp -d "${TMPDIR:-/tmp}/onibi-terminal-smoke.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

results="$tmp/results.tsv"
failures=0
skips=0
passes=0

printf 'app\tcoverage\tstatus\tnote\n' >"$results"
printf 'surface: %s\n' "$surface"
printf '%-28s %-12s %-6s %s\n' "app" "coverage" "status" "note"

record() {
  app="$1"
  coverage="$2"
  status="$3"
  note="$4"
  printf '%s\t%s\t%s\t%s\n' "$app" "$coverage" "$status" "$note" >>"$results"
  printf '%-28s %-12s %-6s %s\n' "$app" "$coverage" "$status" "$note"
  case "$status" in
    PASS) passes=$((passes + 1)) ;;
    SKIP) skips=$((skips + 1)) ;;
    FAIL) failures=$((failures + 1)) ;;
  esac
}

have() {
  command -v "$1" >/dev/null 2>&1
}

skip_or_fail() {
  app="$1"
  coverage="$2"
  note="$3"
  if "$strict"; then
    record "$app" "$coverage" FAIL "$note"
  else
    record "$app" "$coverage" SKIP "$note"
  fi
}

run_vim_like() {
  app="$1"
  bin="$2"
  script="$tmp/$app.vim"
  out="$tmp/$app.txt"
  cat >"$script" <<'VIM'
set nomore
call setline(1, ['onibi-terminal-smoke', 'editor-write-ok'])
write
quitall!
VIM
  if "$bin" -Nu NONE -n -es "$out" -S "$script" >/dev/null 2>&1 && grep -qx 'editor-write-ok' "$out"; then
    record "$app" "auto" PASS "batch edit/write ok"
  else
    record "$app" "auto" FAIL "batch edit/write failed"
  fi
}

run_emacs() {
  out="$tmp/emacs.txt"
  if emacs --batch --eval "(with-temp-file \"$out\" (insert \"onibi-terminal-smoke\neditor-write-ok\n\"))" >/dev/null 2>&1 && grep -qx 'editor-write-ok' "$out"; then
    record "emacs" "auto" PASS "batch edit/write ok"
  else
    record "emacs" "auto" FAIL "batch edit/write failed"
  fi
}

run_tmux() {
  session="onibi-terminal-smoke-$$-$RANDOM"
  tmux kill-session -t "$session" >/dev/null 2>&1 || true
  if tmux new-session -d -s "$session" "printf 'onibi-tmux-ready'; sleep 30" >/dev/null 2>&1; then
    sleep 1
    capture="$(tmux capture-pane -pt "$session" -S - -E - 2>/dev/null || true)"
    tmux kill-session -t "$session" >/dev/null 2>&1 || true
    case "$capture" in
      *onibi-tmux-ready*) record "tmux" "auto" PASS "session capture ok" ;;
      *) record "tmux" "auto" FAIL "session capture missing marker" ;;
    esac
  else
    record "tmux" "auto" FAIL "session start failed"
  fi
}

run_less() {
  fixture="$tmp/less.txt"
  printf 'onibi less smoke\nsearch-token\n' >"$fixture"
  if LESS=FRX less "$fixture" </dev/null >/dev/null 2>&1; then
    record "less" "auto" PASS "pager open/exit ok"
  else
    record "less" "auto" FAIL "pager open/exit failed"
  fi
}

run_fzf() {
  if result="$(printf 'alpha\nbeta\n' | fzf --filter beta 2>/dev/null)" && [ "$result" = "beta" ]; then
    record "fzf" "auto" PASS "filter selection ok"
  else
    record "fzf" "auto" FAIL "filter selection failed"
  fi
}

check_version() {
  app="$1"
  if "$app" --version >/dev/null 2>&1; then
    record "$app" "manual" PASS "installed; run phone interaction manually"
  else
    record "$app" "manual" FAIL "--version failed"
  fi
}

if have vim; then run_vim_like "vim" "vim"; else skip_or_fail "vim" "auto" "missing command: vim"; fi
if have nvim; then run_vim_like "nvim" "nvim"; else skip_or_fail "nvim" "auto" "missing command: nvim"; fi
if have emacs; then run_emacs; else skip_or_fail "emacs" "auto" "missing command: emacs"; fi
if have tmux; then run_tmux; else skip_or_fail "tmux" "auto" "missing command: tmux"; fi
if have less; then run_less; else skip_or_fail "less" "auto" "missing command: less"; fi
if have htop; then check_version "htop"; else skip_or_fail "htop" "manual" "missing command: htop"; fi

agent_tools=""
for tool in claude codex opencode; do
  if have "$tool"; then
    agent_tools="${agent_tools}${agent_tools:+ }$tool"
  fi
done
if [ -n "$agent_tools" ]; then
  record "claude/codex/opencode" "manual" PASS "installed: $agent_tools"
else
  skip_or_fail "claude/codex/opencode" "manual" "missing commands: claude codex opencode"
fi

if have ranger; then check_version "ranger"; else skip_or_fail "ranger" "manual" "missing command: ranger"; fi
if have fzf; then run_fzf; else skip_or_fail "fzf" "auto" "missing command: fzf"; fi
if have gum; then check_version "gum"; else skip_or_fail "gum" "manual" "missing command: gum"; fi

if [ -n "$out_dir" ]; then
  mkdir -p "$out_dir"
  cp "$results" "$out_dir/results.tsv"
  printf 'wrote %s\n' "$out_dir/results.tsv"
fi

printf 'summary: %s pass, %s skip, %s fail\n' "$passes" "$skips" "$failures"
if [ "$failures" -gt 0 ]; then
  exit 1
fi
