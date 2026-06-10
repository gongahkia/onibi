// Command onibi-notify is the tiny client invoked by agent and shell hooks.
// Writes a JSON event to the daemon's local Unix-domain socket. Fails open:
// if the daemon is down, the socket is missing, or any transport error
// occurs, exits 0 silently so we never block the user's shell or coding
// agent.
//
// Identity is supplied by env vars set when the daemon spawned the agent:
//
//	ONIBI_SOCK         absolute path to intake socket (required)
//	ONIBI_SESSION_ID   stable id of this session (optional)
//
// Flags fill in the event payload:
//
//	--type <name>      required (agent_done, agent_awaiting, cmd_done, ...)
//	--status <int>     exit code (cmd_done)
//	--cmd <string>     command line (cmd_done)
//	--elapsed-ms <int> elapsed time (cmd_done)
//	--text <string>    human-readable detail
//	--tail-stdin       read up to 64 KiB of tail from stdin
package main

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/gongahkia/onibi/internal/intake"
)

const maxTailBytes = 64 << 10

func main() {
	// fail-open contract: any error path returns 0
	defer func() { _ = recover() }()

	typ := flag.String("type", "", "event type")
	status := flag.Int("status", 0, "exit status (cmd_done)")
	cmd := flag.String("cmd", "", "command line (cmd_done)")
	elapsedMS := flag.Int64("elapsed-ms", 0, "elapsed milliseconds")
	text := flag.String("text", "", "human-readable detail")
	tailStdin := flag.Bool("tail-stdin", false, "read tail from stdin")
	approvalID := flag.String("approval-id", "", "approval id (phase 3)")
	tool := flag.String("tool", "", "tool name (phase 3)")
	flag.Parse()

	if *typ == "" {
		// no event type, nothing to send
		os.Exit(0)
	}

	sock := strings.TrimSpace(os.Getenv("ONIBI_SOCK"))
	if sock == "" {
		// daemon not active for this process — silently no-op
		os.Exit(0)
	}

	ev := intake.Event{
		Type:       *typ,
		Session:    os.Getenv("ONIBI_SESSION_ID"),
		PID:        os.Getppid(),
		Status:     *status,
		Cmd:        *cmd,
		Elapsed:    *elapsedMS,
		Text:       *text,
		ApprovalID: *approvalID,
		Tool:       *tool,
	}

	if *tailStdin {
		// cap at maxTailBytes to avoid pathological inputs
		lim := io.LimitReader(os.Stdin, maxTailBytes)
		b, _ := io.ReadAll(lim)
		ev.Tail = string(b)
	}

	_ = intake.Send(sock, ev)
	os.Exit(0)
}
