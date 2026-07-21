//go:build !onibi_remote

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log/slog"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/chatout"
	"github.com/gongahkia/onibi/internal/irc"
	"github.com/gongahkia/onibi/internal/render"
)

func (d *Daemon) runIRCBridge(ctx context.Context) error {
	p := irc.NewProvider(d.IRCClient, d.IRCNick, d.IRCOwnerNick, d.IRCOwnerAccount)
	if err := d.configureIRCProvider(ctx, p); err != nil {
		return err
	}
	go d.forwardIRCApprovals(ctx, p)
	go d.forwardIRCSessionTail(ctx, p)
	return p.Reconnect(ctx)
}

func (d *Daemon) configureIRCProvider(ctx context.Context, p chatout.Provider) error {
	if provider, ok := p.(*irc.Provider); ok {
		provider.Audit = d.auditIRCInteraction
	}
	if err := p.OnInboundText(func(text string, sender chatout.Sender) {
		actor := ircActorID(sender.ID)
		sessionID := d.providerTargetSessionID("")
		out, err := d.handleProviderTextFor(ctx, "", text, actor, "irc")
		if err != nil {
			out = "Input failed: " + err.Error()
		}
		if err := p.SendText(ctx, out); err != nil {
			d.Log.Warn("irc output", slog.Any("err", err))
			return
		}
		d.audit(ctx, "provider.irc.output", sessionID, out, actor, "provider=irc direction=out")
	}); err != nil {
		return err
	}
	if err := p.OnDecision("*", func(decision chatout.Decision) {
		if err := d.decideProviderApproval(ctx, decision.ApprovalID, approvalVerdictForAction(decision.Verdict), ircActorID(decision.Sender.ID)); err != nil {
			d.Log.Warn("irc approval", "approval_id", decision.ApprovalID, slog.Any("err", err))
		}
	}); err != nil {
		return err
	}
	return nil
}

func (d *Daemon) forwardIRCApprovals(ctx context.Context, p chatout.Provider) {
	if d.Queue == nil {
		return
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		d.Log.Warn("irc approval subscribe", slog.Any("err", err))
		return
	}
	defer unsub()
	sent := map[string]bool{}
	sendPending := func() {
		pending, err := d.Queue.Pending(ctx)
		if err != nil {
			d.Log.Warn("irc approval replay", slog.Any("err", err))
			return
		}
		for _, a := range pending {
			if sent[a.ID] {
				continue
			}
			risk := approval.ClassifyRisk(a.Tool, a.InputJSON)
			if _, err := p.SendApproval(ctx, chatout.ApprovalRequest{ID: a.ID, SessionID: a.SessionID, Agent: a.Agent, Tool: a.Tool, InputJSON: a.InputJSON, RiskLevel: risk.Level}); err != nil {
				d.Log.Warn("irc approval send", "approval_id", a.ID, slog.Any("err", err))
				continue
			}
			sent[a.ID] = true
		}
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	sendPending()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendPending()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type == approval.EventRequested {
				sendPending()
			}
		}
	}
}

func (d *Daemon) forwardIRCSessionTail(ctx context.Context, p *irc.Provider) {
	if p == nil {
		return
	}
	if err := p.WaitForOwner(ctx); err != nil {
		return
	}
	s, err := d.sessionForRPCTarget("")
	if err != nil || s == nil || s.Host == nil {
		if err != nil {
			d.Log.Warn("irc tail session", slog.Any("err", err))
		}
		return
	}
	_, chunks, unsub := s.Host.SubscribeLive(ctx, 128)
	defer unsub()
	if err := p.TailStream(ctx, s.ID, d.ircTailChunks(ctx, chunks)); err != nil && !errors.Is(err, context.Canceled) {
		d.Log.Warn("irc tail", slog.Any("err", err))
	}
}

func (d *Daemon) ircTailChunks(ctx context.Context, chunks <-chan []byte) <-chan []byte {
	out := make(chan []byte)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-chunks:
				if !ok {
					return
				}
				text := d.prepareProviderOutputFor("irc", string(render.StripANSI(chunk)))
				if strings.TrimSpace(text) == "" {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- []byte(text):
				}
			}
		}
	}()
	return out
}

func (d *Daemon) auditIRCInteraction(ctx context.Context, item chatout.AuditInteraction) error {
	meta, err := json.Marshal(item.Meta)
	if err != nil {
		return err
	}
	d.audit(ctx, item.Kind, item.SessionID, "", ircActorID(item.Sender.ID), "provider=irc meta="+string(meta))
	return nil
}

func ircActorID(account string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(account))))
	id := int64(h.Sum64() & ((uint64(1) << 63) - 1))
	if id == 0 {
		return 1
	}
	return id
}
