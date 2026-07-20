//go:build !onibi_remote

package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/web"
)

func (d *Daemon) approvalSessionID(ctx context.Context, id string) string {
	if d == nil || d.Queue == nil || id == "" {
		return ""
	}
	a, err := d.Queue.Get(ctx, id)
	if err != nil {
		return ""
	}
	return a.SessionID
}

func (d *Daemon) providerTargetSessionID(target string) string {
	if d == nil {
		return ""
	}
	s, err := d.sessionForRPCTarget(target)
	if err != nil {
		return ""
	}
	return s.ID
}

func (d *Daemon) runWebPushNotifier(ctx context.Context) {
	d.forwardNotifyApprovals(ctx, func(a *approval.Approval) {
		web.SendApprovalPushNotifications(ctx, d.DB, a, d.Log)
	})
}

func (d *Daemon) forwardNotifyApprovals(ctx context.Context, send func(*approval.Approval)) {
	if d.Queue == nil {
		return
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		if d.Log != nil {
			d.Log.Warn("notify approval subscribe failed", "err", err)
		}
		return
	}
	defer unsub()
	sent := map[string]bool{}
	sendOnce := func(a *approval.Approval) {
		if a == nil || sent[a.ID] {
			return
		}
		sent[a.ID] = true
		send(a)
	}
	pending, err := d.Queue.Pending(ctx)
	if err != nil {
		d.Log.Warn("notify approval replay failed", slog.Any("err", err))
	} else {
		for _, a := range pending {
			select {
			case <-ctx.Done():
				return
			default:
				sendOnce(a)
			}
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type == approval.EventRequested {
				a := ev.Approval
				sendOnce(&a)
			}
		}
	}
}

func (d *Daemon) handleProviderText(ctx context.Context, target, text string, actor int64) (string, error) {
	return d.handleProviderTextFor(ctx, target, text, actor, "")
}

func (d *Daemon) handleProviderTextFor(ctx context.Context, target, text string, actor int64, provider string) (string, error) {
	if handled, reply := d.handleProviderTextCommand(ctx, text, actor); handled {
		return d.prepareProviderOutputFor(provider, reply), nil
	}
	out, err := d.SendSessionTextAndCapture(ctx, target, text, true)
	return d.prepareProviderOutputFor(provider, out), err
}

func (d *Daemon) handleProviderTextCommand(ctx context.Context, text string, actor int64) (bool, string) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false, ""
	}
	verb := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	var verdict approval.Verdict
	switch verb {
	case "approve", "ap":
		verdict = approval.VerdictApprove
	case "deny", "dn":
		verdict = approval.VerdictDeny
	default:
		return false, ""
	}
	if len(fields) < 2 {
		return true, "Approval id required."
	}
	id := fields[1]
	if err := d.decideProviderApproval(ctx, id, verdict, actor); err != nil {
		return true, fmt.Sprintf("Approval %s failed: %v", id, err)
	}
	return true, fmt.Sprintf("Approval %s %s.", id, verdict)
}

func approvalVerdictForAction(action string) approval.Verdict {
	switch strings.ToLower(action) {
	case "approve", "ap":
		return approval.VerdictApprove
	case "deny", "dn":
		return approval.VerdictDeny
	default:
		return ""
	}
}

func (d *Daemon) decideProviderApproval(ctx context.Context, id string, verdict approval.Verdict, actor int64) error {
	if d.Queue == nil || strings.TrimSpace(id) == "" {
		return errors.New("approval queue/id required")
	}
	_, err := d.Queue.DecideIdempotently(ctx, id, verdict, "", fmt.Sprintf("provider %s", verdict), actor)
	return err
}
