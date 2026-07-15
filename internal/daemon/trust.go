package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/trust"
	"github.com/gongahkia/onibi/internal/web"
)

func (d *Daemon) startTrustWatcher(ctx context.Context, wg *sync.WaitGroup) error {
	if d == nil || d.Registry == nil {
		return nil
	}
	w, err := trust.NewWatcher(func(ev trust.WatchEvent) {
		d.handleTrustWatchEvent(ctx, ev)
	})
	if err != nil {
		return err
	}
	d.Trust = w
	d.syncTrustRoots(w)
	wg.Add(2)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.syncTrustRoots(w)
			}
		}
	}()
	return nil
}

func (d *Daemon) syncTrustRoots(w *trust.Watcher) {
	if w == nil || d.Registry == nil {
		return
	}
	for _, s := range d.Registry.List() {
		if s == nil || s.CWD == "" || s.Ended() {
			continue
		}
		if err := w.AddRoot(s.CWD); err != nil {
			d.audit(context.Background(), "trust.policy.watch_error", s.ID, "", 0, "root="+s.CWD+" err="+err.Error())
		}
	}
}

func (d *Daemon) handleTrustWatchEvent(ctx context.Context, ev trust.WatchEvent) {
	if ev.Err != nil {
		d.audit(ctx, "trust.policy.error", "", "", 0, fmt.Sprintf("root=%s path=%s err=%s", ev.Root, ev.Path, ev.Err))
		d.publishToast("Trust policy not reloaded: " + ev.Err.Error())
		return
	}
	if ev.Initial {
		return
	}
	d.audit(ctx, "trust.policy.reload", "", "", 0, fmt.Sprintf("root=%s path=%s rules=%d->%d changed=%t",
		ev.Root, ev.Path, len(ev.Previous.Rules), len(ev.Policy.Rules), !reflect.DeepEqual(ev.Previous, ev.Policy)))
}

func (d *Daemon) publishToast(message string) {
	if d == nil || d.Events == nil {
		return
	}
	d.Events.Publish(web.Event{
		Type: "toast",
		Payload: map[string]any{
			"level":   "warning",
			"message": message,
		},
	})
}

func (d *Daemon) AddRuntimeTrustRule(ctx context.Context, req web.TrustRuntimeRequest) (string, error) {
	if d == nil || d.Trust == nil {
		return "", errors.New("trust watcher unavailable")
	}
	if d.Registry == nil {
		return "", errors.New("session registry unavailable")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return "", errors.New("session_id required")
	}
	s, err := d.Registry.Get(sessionID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(s.CWD) == "" {
		return "", errors.New("session cwd required")
	}
	tool := strings.TrimSpace(req.Tool)
	if tool == "" {
		return "", errors.New("tool required")
	}
	path := trustMatchPath(s.CWD, req.Path)
	if path == "" {
		return "", errors.New("path required")
	}
	expires := strings.TrimSpace(req.Expires)
	ttl, err := time.ParseDuration(expires)
	if err != nil || ttl <= 0 {
		return "", errors.New("expires must be a positive duration")
	}
	agent := strings.TrimSpace(req.Agent)
	if agent == "" {
		agent = s.Agent
	}
	rule := trust.RuntimeRule(trust.Match{Tool: tool, Path: path, Agent: agent}, trust.EffectAutoApprove, ttl, time.Now())
	rule.ExpiresRaw = expires
	if err := d.Trust.AddRuntimeRule(s.CWD, rule); err != nil {
		return "", err
	}
	d.audit(ctx, "trust.runtime.add", s.ID, "", 0, fmt.Sprintf("tool=%s path=%s agent=%s expires=%s", tool, path, agent, expires))
	msg := fmt.Sprintf("Auto-approving %s in %s for %s.", tool, path, expires)
	d.publishToast(msg)
	return msg, nil
}

func (d *Daemon) handleTrustApproval(ctx context.Context, s *Session, ev intake.Event) (intake.Response, bool) {
	if d == nil || d.Trust == nil || d.Queue == nil || s == nil {
		return intake.Response{}, false
	}
	if _, ok := adapters.ContractFor(ev.Agent); !ok {
		return intake.Response{}, false
	}
	p, ok := d.Trust.Policy(s.CWD)
	if !ok {
		return intake.Response{}, false
	}
	req := trustRequestForApproval(s, ev)
	evaluation := p.Explain(req)
	rule, ok := evaluation.Result()
	if !ok {
		return intake.Response{}, false
	}
	switch rule.Effect {
	case trust.EffectAutoApprove:
		return d.finishTrustApproval(ctx, s, ev, rule, evaluation, approval.VerdictApprove, "auto-approved by trust policy", "trust.auto_approve", "Auto-approved "+ev.Tool+" by trust policy"), true
	case trust.EffectDeny:
		return d.finishTrustApproval(ctx, s, ev, rule, evaluation, approval.VerdictDeny, "denied by trust policy", "trust.deny", "Denied "+ev.Tool+" by trust policy"), true
	case trust.EffectAlwaysPrompt:
		d.audit(ctx, "trust.always_prompt", s.ID, ev.InputJSON, 0, trustApprovalAuditDetail(rule, req, "", evaluation))
		return intake.Response{}, false
	default:
		return intake.Response{}, false
	}
}

func (d *Daemon) finishTrustApproval(ctx context.Context, s *Session, ev intake.Event, rule trust.Rule, evaluation trust.Evaluation, verdict approval.Verdict, reason, action, toast string) intake.Response {
	req := trustRequestForApproval(s, ev)
	agent := strings.TrimSpace(ev.Agent)
	if agent == "" && s != nil {
		agent = s.Agent
	}
	id, ch, err := d.Queue.RequestSilent(ctx, ev.Session, agent, ev.Tool, ev.InputJSON)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}
	}
	if err := d.Queue.Decide(ctx, id, verdict, "", reason, 0); err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}
	}
	d.audit(ctx, action, s.ID, ev.InputJSON, 0, trustApprovalAuditDetail(rule, req, id, evaluation))
	d.publishToast(toast)
	select {
	case dec := <-ch:
		return responseForDecision(dec, ev)
	default:
		if verdict == approval.VerdictApprove {
			return intake.Response{Decision: string(approval.VerdictApprove)}
		}
		return intake.Response{Decision: string(approval.VerdictDeny), Reason: reason}
	}
}

func trustApprovalAuditDetail(rule trust.Rule, req trust.Request, approvalID string, evaluation trust.Evaluation) string {
	trace := make([]string, 0, len(evaluation.Trace))
	for _, item := range evaluation.Trace {
		trace = append(trace, item.Rule.ID+":"+item.Outcome)
	}
	traceJSON, err := json.Marshal(evaluation.Trace)
	if err != nil {
		traceJSON = []byte("[]")
	}
	return fmt.Sprintf("rule=%s tool=%s path=%s approval=%s trace=%s trace_json=%s", trustRuleAuditID(rule), req.Tool, req.Path, approvalID, strings.Join(trace, ","), traceJSON)
}

func trustRuleAuditID(rule trust.Rule) string {
	if strings.TrimSpace(rule.ID) == "" {
		return "unknown"
	}
	return rule.ID
}

func trustRequestForApproval(s *Session, ev intake.Event) trust.Request {
	path := strings.TrimSpace(ev.FilePath)
	if path == "" {
		path = approval.ExtractDetails(ev.Tool, ev.InputJSON).FilePath
	}
	agent := strings.TrimSpace(ev.Agent)
	if agent == "" && s != nil {
		agent = s.Agent
	}
	root := ""
	if s != nil {
		root = s.CWD
	}
	if root == "" {
		root = ev.CWD
	}
	return trust.Request{
		Tool:  ev.Tool,
		Path:  trustMatchPath(root, path),
		Agent: agent,
	}
}

func trustMatchPath(root, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) && strings.TrimSpace(root) != "" {
		if rel, err := filepath.Rel(root, path); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}
