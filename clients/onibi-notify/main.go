// Command onibi-notify is a tiny client invoked by agent and shell hooks.
// Writes a JSON event to the daemon's local Unix-domain socket. Fails open:
// if the daemon is down or the socket is missing, exits 0 silently so we
// never block the user's shell or coding agent. Phase 2.
//
// Usage:
//
//	onibi-notify --type cmd_done --status "$?" --cmd "$LAST_CMD" --tail "..."
//	onibi-notify --type agent_done --pane "$TMUX_PANE"
//	onibi-notify --type agent_awaiting --pane "$TMUX_PANE" --text "$PROMPT"
package main

import "os"

func main() {
	// stub — phase 2 implements socket dial, JSON event write, fail-open
	// short-circuit (exit 0 on any error). Keep this binary tiny: no
	// dependencies beyond stdlib so it's a fast startup for shell-prompt
	// invocation.
	os.Exit(0)
}
