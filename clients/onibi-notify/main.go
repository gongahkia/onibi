// Command onibi-notify is the client invoked by agent and shell hooks. Two
// modes:
//
//  1. Fire-and-forget (default). Writes a JSON event to the daemon's local
//     Unix socket. Fails open: if the daemon is down, exit 0 silently.
//
//  2. RPC (--wait, used for approval_request). Reads the agent's tool
//     payload from stdin, sends it, blocks for the daemon's decision,
//     writes the agent-appropriate response JSON to stdout, exits with a
//     code the agent's hook system understands:
//        - approve  → exit 0
//        - edited   → exit 0 with updatedInput in stdout JSON
//        - deny     → exit 2 with reason on stderr
//        - expired  → exit 2 with "Approval expired" on stderr
//        - cancelled→ exit 0 (let the tool proceed normally — daemon
//                     unavailable shouldn't block work)
//
// Identity is supplied by env vars set when the daemon spawned the agent:
//
//	ONIBI_SOCK         absolute path to intake socket (required)
//	ONIBI_SESSION_ID   stable id of this session (optional)
//
// Flags:
//
//	--type <name>         required (agent_done, agent_awaiting,
//	                      cmd_done, approval_request, ...)
//	--wait                RPC mode (currently approval_request only)
//	--status <int>        exit code (cmd_done)
//	--cmd <string>        command line (cmd_done)
//	--elapsed-ms <int>    elapsed (cmd_done)
//	--text <string>       human-readable detail
//	--tail-stdin          read up to 64 KiB of tail from stdin
package main

import (
	"flag"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/adapters/claude"
	"github.com/gongahkia/onibi/internal/intake"
)

const (
	maxTailBytes    = 64 << 10
	approvalTimeout = 6 * time.Minute // approval TTL is 5min + slack
)

func main() {
	typ := flag.String("type", "", "event type")
	wait := flag.Bool("wait", false, "block for response (RPC mode)")
	status := flag.Int("status", 0, "exit status (cmd_done)")
	cmd := flag.String("cmd", "", "command line (cmd_done)")
	elapsedMS := flag.Int64("elapsed-ms", 0, "elapsed milliseconds")
	text := flag.String("text", "", "human-readable detail")
	tailStdin := flag.Bool("tail-stdin", false, "read tail from stdin (fire-and-forget mode)")
	flag.Parse()

	if *typ == "" {
		os.Exit(0)
	}
	sock := strings.TrimSpace(os.Getenv("ONIBI_SOCK"))
	if sock == "" {
		// daemon not active — silently no-op so we don't block hooks
		os.Exit(0)
	}

	if *wait {
		runWait(sock, *typ)
		return
	}

	// fire-and-forget event
	ev := intake.Event{
		Type:    *typ,
		Session: os.Getenv("ONIBI_SESSION_ID"),
		PID:     os.Getppid(),
		Status:  *status,
		Cmd:     *cmd,
		Elapsed: *elapsedMS,
		Text:    *text,
	}
	if *tailStdin {
		lim := io.LimitReader(os.Stdin, maxTailBytes)
		b, _ := io.ReadAll(lim)
		ev.Tail = string(b)
	}
	_ = intake.Send(sock, ev)
	os.Exit(0)
}

// runWait handles RPC mode. Claude Code's PreToolUse hook supplies the
// tool name + input on stdin as one JSON object: {"tool_name": "...",
// "tool_input": {...}}. We extract those, ask the daemon, then emit Claude's
// expected JSON on stdout.
func runWait(sock, typ string) {
	if typ != "approval_request" {
		// not implemented — fail open
		os.Exit(0)
	}

	// read Claude's PreToolUse stdin payload (best effort)
	tool, inputJSON := claude.ParsePreToolUse(os.Stdin)

	ev := intake.Event{
		Type:      intake.TypeApprovalRequest,
		Session:   os.Getenv("ONIBI_SESSION_ID"),
		PID:       os.Getppid(),
		Tool:      tool,
		InputJSON: inputJSON,
	}

	resp, err := intake.Request(sock, ev, approvalTimeout)
	if err != nil {
		// daemon down or any other error — fail open (let the tool proceed
		// normally; the user can still cancel manually if needed). This
		// matches our fail-open contract from §1 hard rules.
		os.Exit(0)
	}

	result := claude.PreToolUseResponse(resp)
	if result.Stdout != "" {
		_, _ = os.Stdout.WriteString(result.Stdout)
	}
	if result.Stderr != "" {
		_, _ = os.Stderr.WriteString(result.Stderr)
	}
	os.Exit(result.ExitCode)
}
