//go:build !onibi_remote

package daemon

import (
	"context"
	"fmt"
	"hash/fnv"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/chatout"
	"github.com/gongahkia/onibi/internal/irc"
)

type ircBridge struct {
	d        *Daemon
	provider *irc.Provider

	mu       sync.Mutex
	target   string
	lastTail map[string]string
}

func (d *Daemon) runIRCBridge(ctx context.Context) error {
	p := irc.NewProvider(d.IRCClient, d.IRCOwnerNick, d.IRCOwnerToken)
	b := &ircBridge{d: d, provider: p, lastTail: map[string]string{}}
	p.Audit = func(ctx context.Context, item chatout.AuditInteraction) error {
		b.d.audit(ctx, item.Kind, item.SessionID, item.Payload, ircActorID(item.Sender.ID), "nick="+item.Sender.ID)
		return nil
	}
	if err := p.OnInboundText(func(text string, sender chatout.Sender) { b.handleText(ctx, text, sender) }); err != nil {
		return err
	}
	if err := p.OnDecision("*", func(decision chatout.Decision) { b.handleDecision(ctx, decision) }); err != nil {
		return err
	}
	go b.forwardApprovals(ctx)
	go b.tailSessions(ctx)
	return p.Connect(ctx)
}

func (b *ircBridge) handleText(ctx context.Context, text string, sender chatout.Sender) {
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		b.handleCommand(ctx, text, sender)
		return
	}
	target := b.currentTarget()
	out, err := b.d.SendSessionTextAndCapture(ctx, target, text, true)
	if err != nil {
		b.send(ctx, "Input failed: "+err.Error())
		return
	}
	b.sendOutput(ctx, b.d.providerTargetSessionID(target), out)
}

func (b *ircBridge) handleCommand(ctx context.Context, text string, sender chatout.Sender) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return
	}
	cmd := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), fields[0]))
	switch cmd {
	case "help":
		b.send(ctx, "Onibi IRC: !onibi <token> <text>; /status /sessions /target <id> /new <agent> /peek /interrupt /kill /approve <id> /deny <id>")
	case "status":
		b.send(ctx, b.d.pingText(ctx, -1)+"\n"+b.sessionsText())
	case "sessions":
		b.send(ctx, b.sessionsText())
	case "target":
		s, err := b.d.sessionByID(arg)
		if err != nil {
			b.send(ctx, "Target failed: "+err.Error())
			return
		}
		b.setTarget(s.ID)
		b.send(ctx, "Target: "+s.Name+" ("+s.ID+")")
	case "new":
		b.handleNew(ctx, arg)
	case "peek", "text":
		out, err := b.d.CaptureSessionText(ctx, b.currentTarget())
		if err != nil {
			b.send(ctx, "Peek failed: "+err.Error())
			return
		}
		b.sendOutput(ctx, b.d.providerTargetSessionID(b.currentTarget()), out)
	case "interrupt":
		if err := b.d.ControlSession(ctx, b.currentTarget(), "interrupt"); err != nil {
			b.send(ctx, "Interrupt failed: "+err.Error())
			return
		}
		b.send(ctx, "Interrupted.")
	case "kill":
		if err := b.d.ControlSession(ctx, b.currentTarget(), "kill"); err != nil {
			b.send(ctx, "Kill failed: "+err.Error())
			return
		}
		b.send(ctx, "Killed.")
	case "approve", "ap", "deny", "dn":
		b.handleDecision(ctx, chatout.Decision{ApprovalID: firstArg(arg), Verdict: verdictForIRCCommand(cmd), Sender: sender})
	default:
		b.send(ctx, "Unknown command. Use /help.")
	}
}

func (b *ircBridge) handleDecision(ctx context.Context, decision chatout.Decision) {
	verdict := approval.Verdict(decision.Verdict)
	if decision.ApprovalID == "" || (verdict != approval.VerdictApprove && verdict != approval.VerdictDeny) {
		b.send(ctx, "Usage: /approve <id> or /deny <id>")
		return
	}
	if _, err := b.d.Queue.DecideIdempotently(ctx, decision.ApprovalID, verdict, "", "decided from IRC", ircActorID(decision.Sender.ID)); err != nil {
		b.send(ctx, "Approval failed: "+err.Error())
		return
	}
	b.send(ctx, "Approval "+decision.ApprovalID+": "+string(verdict))
}

func (b *ircBridge) handleNew(ctx context.Context, arg string) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		fields = []string{"shell"}
	}
	bin, agent, args, ok := agentCommand(strings.ToLower(fields[0]), fields[1:])
	if !ok {
		b.send(ctx, "Unsupported target. Try /new shell, /new claude, or /new codex.")
		return
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		b.send(ctx, bin+" not found in PATH")
		return
	}
	s, err := b.d.StartTmuxSession(ctx, "", agent, path, args, "")
	if err != nil {
		b.send(ctx, "Start failed: "+err.Error())
		return
	}
	b.setTarget(s.ID)
	b.send(ctx, "Started "+s.Name+" ("+s.ID+").")
}

func (b *ircBridge) forwardApprovals(ctx context.Context) {
	if b.d.Queue == nil {
		return
	}
	ch, unsub, err := b.d.Queue.Subscribe()
	if err != nil {
		return
	}
	defer unsub()
	if pending, err := b.d.Queue.Pending(ctx); err == nil {
		for _, a := range pending {
			b.sendApproval(ctx, a)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Type == approval.EventRequested {
				a := event.Approval
				b.sendApproval(ctx, &a)
			}
		}
	}
}

func (b *ircBridge) sendApproval(ctx context.Context, a *approval.Approval) {
	if a == nil {
		return
	}
	text := formatApprovalWithPolicy(a, b.d.providerOutputPolicy("irc"))
	text += "\nReply: !onibi <token> /approve " + a.ID + " or /deny " + a.ID
	b.send(ctx, text)
}

func (b *ircBridge) tailSessions(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			target := b.currentTarget()
			sessionID := b.d.providerTargetSessionID(target)
			if sessionID == "" {
				continue
			}
			out, err := b.d.CaptureSessionText(ctx, target)
			if err == nil {
				b.sendOutput(ctx, sessionID, out)
			}
		}
	}
}

func (b *ircBridge) sendOutput(ctx context.Context, sessionID, text string) {
	text = b.d.prepareProviderOutputFor("irc", text)
	if strings.TrimSpace(text) == "" {
		return
	}
	b.mu.Lock()
	if b.lastTail[sessionID] == text {
		b.mu.Unlock()
		return
	}
	b.lastTail[sessionID] = text
	b.mu.Unlock()
	b.d.audit(ctx, "provider.irc.tail_chunk", sessionID, text, 0, "owner="+b.d.IRCOwnerNick)
	b.send(ctx, text)
}

func (b *ircBridge) send(ctx context.Context, text string) {
	if err := b.provider.SendText(ctx, text); err != nil && b.d.Log != nil {
		b.d.Log.Warn("irc send failed", "err", err)
	}
}

func (b *ircBridge) currentTarget() string {
	b.mu.Lock()
	target := b.target
	b.mu.Unlock()
	if target != "" {
		return target
	}
	live := b.d.liveSessions()
	if len(live) == 1 {
		return live[0].ID
	}
	return ""
}

func (b *ircBridge) setTarget(id string) {
	b.mu.Lock()
	b.target = id
	b.mu.Unlock()
}

func (b *ircBridge) sessionsText() string {
	live := b.d.liveSessions()
	if len(live) == 0 {
		return "No active sessions. Try /new shell."
	}
	target := b.currentTarget()
	var out strings.Builder
	out.WriteString("Sessions:")
	for _, s := range live {
		mark := " "
		if s.ID == target {
			mark = "*"
		}
		fmt.Fprintf(&out, "\n%s %s %s", mark, s.ID, s.Name)
	}
	return out.String()
}

func verdictForIRCCommand(cmd string) string {
	if cmd == "approve" || cmd == "ap" {
		return "approve"
	}
	return "deny"
}

func firstArg(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func ircActorID(nick string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(nick))))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}
