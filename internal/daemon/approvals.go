package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/intake"
)

func (d *Daemon) handleApprovalRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	s, reason := d.sessionForEvent(ev)
	if s == nil {
		d.auditIgnoredHook(ctx, "approval.ignored", ev, reason)
		return intake.Response{Decision: "cancelled", Reason: "unmanaged or unknown Onibi session"}, nil
	}
	ev.Session = s.ID
	d.appendEventOutput(s, ev)
	approvalID, ch, err := d.Queue.Request(ctx, ev.Session, ev.Agent, ev.Tool, ev.InputJSON)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
	}
	a, getErr := d.Queue.Get(ctx, approvalID)
	if getErr != nil {
		return intake.Response{Decision: "cancelled", Reason: getErr.Error()}, nil
	}
	d.noteAnomaly(ctx, "approval.request")
	if isHighRiskApproval(a) {
		d.noteAnomaly(ctx, "approval.high_risk")
	}
	d.audit(ctx, "approval.request", ev.Session, ev.InputJSON, 0, fmt.Sprintf("tool=%s id=%s", ev.Tool, approvalID))
	select {
	case dec := <-ch:
		return responseForDecision(dec, ev), nil
	case <-ctx.Done():
		_ = d.Queue.Cancel(context.Background(), approvalID, "daemon shutdown")
		return intake.Response{Decision: "cancelled", Reason: "daemon shutdown"}, nil
	}
}

func (d *Daemon) handleDemoApprovalRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	approvalID, ch, err := d.startDemoApproval(ctx, ev)
	if err != nil {
		return intake.Response{}, err
	}
	select {
	case dec := <-ch:
		return responseForDecision(dec, ev), nil
	case <-ctx.Done():
		_ = d.Queue.Cancel(context.Background(), approvalID, "demo approval cancelled")
		return intake.Response{Decision: "cancelled", Reason: "demo approval cancelled"}, nil
	}
}

func (d *Daemon) startDemoApproval(ctx context.Context, ev intake.Event) (string, <-chan approval.Decision, error) {
	tool := strings.TrimSpace(ev.Tool)
	if tool == "" {
		tool = "Bash"
	}
	inputJSON := strings.TrimSpace(ev.InputJSON)
	if inputJSON == "" {
		inputJSON = `{"command":"echo onibi demo approval"}`
	}
	agent := strings.TrimSpace(ev.Agent)
	if agent == "" {
		agent = "demo"
	}
	sessionID := strings.TrimSpace(ev.Session)
	if sessionID == "" {
		sessionID = "demo"
	}
	approvalID, ch, err := d.Queue.Request(ctx, sessionID, agent, tool, inputJSON)
	if err != nil {
		return "", nil, err
	}
	d.audit(ctx, "approval.demo", sessionID, inputJSON, 0, "tool="+tool+" id="+approvalID)
	return approvalID, ch, nil
}

func (d *Daemon) RestorePendingApprovals(ctx context.Context) error {
	_ = ctx
	return nil
}

func responseForDecision(dec approval.Decision, ev intake.Event) intake.Response {
	switch dec.Verdict {
	case approval.VerdictApprove:
		return intake.Response{Decision: string(approval.VerdictApprove)}
	case approval.VerdictEdit:
		return intake.Response{Decision: string(approval.VerdictEdit), UpdatedInput: string(dec.UpdatedInput)}
	case approval.VerdictDeny:
		return intake.Response{Decision: "denied", Reason: dec.Reason}
	case approval.VerdictExpire:
		return intake.Response{Decision: "expired", Reason: dec.Reason}
	default:
		return intake.Response{Decision: "cancelled", Reason: dec.Reason}
	}
}

func isHighRiskApproval(a *approval.Approval) bool {
	if a == nil {
		return false
	}
	return approval.ClassifyRisk(a.Tool, a.InputJSON).Level == "high"
}
