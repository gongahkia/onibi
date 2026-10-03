#!/bin/sh
# Run a command with a compact TTY-only loader. Successful command output is
# intentionally hidden; failures replay it so the user can diagnose the issue.

if [ "$#" -lt 2 ]; then
	echo "usage: onibi-loader.sh <label> <command> [args...]" >&2
	exit 64
fi

onibi_loader_label=$1
shift

# CI, pipes, log files, and terminals without ANSI support should receive the
# command's normal output rather than cursor-control escape sequences.
if [ ! -t 1 ] || [ ! -t 2 ] || [ "${TERM:-}" = "dumb" ]; then
	exec "$@"
fi

onibi_loader_log=$(mktemp "${TMPDIR:-/tmp}/onibi-loader.XXXXXX") || exit 1
onibi_loader_child=""

onibi_loader_cleanup() {
	rm -f "$onibi_loader_log"
}

onibi_loader_interrupt() {
	if [ -n "$onibi_loader_child" ]; then
		kill "$onibi_loader_child" 2>/dev/null || true
	fi
	printf '\r\033[2K' >&2
	onibi_loader_cleanup
	exit 130
}

trap onibi_loader_cleanup 0
trap onibi_loader_interrupt HUP INT TERM

"$@" >"$onibi_loader_log" 2>&1 &
onibi_loader_child=$!
onibi_loader_dots=""

while kill -0 "$onibi_loader_child" 2>/dev/null; do
	printf '\r\033[2K👻 %s%s' "$onibi_loader_label" "$onibi_loader_dots" >&2
	case "$onibi_loader_dots" in
		"") onibi_loader_dots="." ;;
		.) onibi_loader_dots=".." ;;
		..) onibi_loader_dots="..." ;;
		*) onibi_loader_dots="" ;;
	esac
	sleep 0.15
done

wait "$onibi_loader_child"
onibi_loader_status=$?
printf '\r\033[2K' >&2

if [ "$onibi_loader_status" -eq 0 ]; then
	printf '    ✓ %s\n' "$onibi_loader_label"
	exit 0
fi

printf '    ✗ %s failed; command output follows:\n' "$onibi_loader_label" >&2
cat "$onibi_loader_log" >&2
exit "$onibi_loader_status"
