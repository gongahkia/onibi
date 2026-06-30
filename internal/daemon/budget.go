package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/web"
)

var errNoSessionProcessGroup = errors.New("session has no process group")

func (d *Daemon) updateClaudeCost(ctx context.Context, s *Session, ev intake.Event) {
	if d == nil || d.Budget == nil || s == nil || !isClaudeAgent(ev.Agent, s.Agent) {
		return
	}
	cost, ok, err := d.updateClaudeCostSnapshot(ctx, s, ev)
	if err != nil {
		d.logClaudeCostError(s, ev, err)
		return
	}
	if !ok {
		return
	}
	d.publishClaudeCost(ctx, s, ev, cost)
}

func (d *Daemon) updateClaudeCostSnapshot(ctx context.Context, s *Session, ev intake.Event) (budget.CostEvent, bool, error) {
	if d == nil || d.Budget == nil || s == nil || !isClaudeAgent(ev.Agent, s.Agent) {
		return budget.CostEvent{}, false, nil
	}
	return d.Budget.Update(claudeCostRef(s, ev))
}

func (d *Daemon) currentClaudeCostSnapshot(ctx context.Context, s *Session, ev intake.Event) (budget.CostEvent, bool) {
	cost, ok, err := d.updateClaudeCostSnapshot(ctx, s, ev)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			d.logClaudeCostError(s, ev, err)
		}
	} else if ok {
		d.publishClaudeCost(ctx, s, ev, cost)
		return cost, true
	}
	if d == nil || d.Budget == nil || s == nil || !isClaudeAgent(ev.Agent, s.Agent) {
		return budget.CostEvent{}, false
	}
	cost, ok, err = d.Budget.Current(claudeCostRef(s, ev))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			d.logClaudeCostError(s, ev, err)
		}
		return budget.CostEvent{}, false
	}
	return cost, ok
}

func (d *Daemon) publishClaudeCost(ctx context.Context, s *Session, ev intake.Event, cost budget.CostEvent) {
	d.Events.Publish(web.Event{Type: "cost.updated", Payload: cost})
	d.audit(ctx, "cost.update", s.ID, "", 0, "model="+cost.Model)
	daily := d.recordBudgetDailyTokens(cost.TS, cost.InputTokens+cost.OutputTokens)
	d.enforceBudgetOverrun(ctx, s, ev, cost, daily)
}

func (d *Daemon) budgetWarningForApproval(ctx context.Context, s *Session, ev intake.Event) *approval.BudgetWarning {
	p, _, ok := d.loadBudgetPolicy(ctx, s, ev)
	if !ok {
		return nil
	}
	cost, costOK := d.currentClaudeCostSnapshot(ctx, s, ev)
	current := int64(0)
	if costOK {
		current = cost.TotalInputTokens + cost.TotalOutputTokens
	}
	daily := d.currentBudgetDailyTokens(time.Now().UTC())
	if current > daily {
		daily = current
	}
	predicted := estimateApprovalTokens(ev)
	if predicted <= 0 {
		predicted = 1
	}
	return budgetWarningForPolicy(p, current, daily, predicted)
}

func (d *Daemon) enforceBudgetOverrun(ctx context.Context, s *Session, ev intake.Event, cost budget.CostEvent, daily int64) {
	p, _, ok := d.loadBudgetPolicy(ctx, s, ev)
	if !ok {
		return
	}
	current := cost.TotalInputTokens + cost.TotalOutputTokens
	warn := budgetWarningForPolicy(p, current, daily, 0)
	if warn == nil || warn.ProjectedTokens <= warn.LimitTokens {
		return
	}
	key := budgetOverrunKey(s.ID, warn.Scope, warn.LimitTokens)
	if !d.markBudgetOverrun(key) {
		return
	}
	d.audit(ctx, "budget.overrun", s.ID, "", 0, fmt.Sprintf("scope=%s tokens=%d limit=%d action=%s", warn.Scope, warn.ProjectedTokens, warn.LimitTokens, warn.OnOverrun))
	d.publishToast(warn.Message + "; action: " + warn.OnOverrun)
	if budget.OverrunAction(warn.OnOverrun) == budget.OverrunWarn {
		return
	}
	if err := d.applyBudgetOverrun(ctx, s, warn); err != nil {
		d.audit(ctx, "budget.overrun_error", s.ID, "", 0, err.Error())
		d.Log.Warn("budget overrun action failed", slog.String("session", s.ID), slog.String("action", warn.OnOverrun), slog.Any("err", err))
	}
}

func (d *Daemon) loadBudgetPolicy(ctx context.Context, s *Session, ev intake.Event) (budget.Policy, string, bool) {
	root := budgetRoot(s, ev)
	if root == "" {
		return budget.Policy{}, "", false
	}
	p, err := budget.LoadPolicy(budget.PolicyPath(root))
	if err != nil {
		d.audit(ctx, "budget.policy.error", "", "", 0, "root="+root+" err="+err.Error())
		d.publishToast("Budget policy not loaded: " + err.Error())
		return budget.Policy{}, root, false
	}
	if p.Global.MaxTokensPerDay <= 0 && p.Session.MaxTokens <= 0 {
		return p, root, false
	}
	return p, root, true
}

func (d *Daemon) recordBudgetDailyTokens(ts time.Time, tokens int64) int64 {
	if d == nil {
		return 0
	}
	if tokens < 0 {
		tokens = 0
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	key := ts.UTC().Format("2006-01-02")
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.budgetDaily == nil {
		d.budgetDaily = map[string]int64{}
	}
	d.budgetDaily[key] += tokens
	return d.budgetDaily[key]
}

func (d *Daemon) currentBudgetDailyTokens(ts time.Time) int64 {
	if d == nil {
		return 0
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	key := ts.UTC().Format("2006-01-02")
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.budgetDaily[key]
}

func (d *Daemon) markBudgetOverrun(key string) bool {
	if d == nil || key == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.budgetOverruns == nil {
		d.budgetOverruns = map[string]bool{}
	}
	if d.budgetOverruns[key] {
		return false
	}
	d.budgetOverruns[key] = true
	return true
}

func (d *Daemon) applyBudgetOverrun(ctx context.Context, s *Session, warn *approval.BudgetWarning) error {
	if s == nil || warn == nil {
		return nil
	}
	switch budget.OverrunAction(warn.OnOverrun) {
	case budget.OverrunKill:
		return d.killSessionForBudget(ctx, s)
	case budget.OverrunInterrupt, "":
		return d.interruptSessionForBudget(ctx, s)
	default:
		return nil
	}
}

func (d *Daemon) interruptSessionForBudget(ctx context.Context, s *Session) error {
	if s == nil {
		return nil
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		return d.ControlSession(ctx, s.ID, "interrupt")
	}
	if err := signalSessionProcessGroup(s, syscall.SIGINT); err == nil || !errors.Is(err, errNoSessionProcessGroup) {
		return err
	}
	if s.Host == nil {
		return errNoSessionProcessGroup
	}
	_, err := s.Host.Write([]byte{3})
	return err
}

func (d *Daemon) killSessionForBudget(ctx context.Context, s *Session) error {
	if s == nil {
		return nil
	}
	if s.Transport == "tmux" && s.TmuxTarget != "" {
		return d.ControlSession(ctx, s.ID, "kill")
	}
	if err := signalSessionProcessGroup(s, syscall.SIGKILL); err == nil {
		if s.Host != nil {
			_ = s.Host.Close()
		}
		d.markSessionEnded(ctx, s)
		return nil
	} else if !errors.Is(err, errNoSessionProcessGroup) {
		return err
	}
	return d.ControlSession(ctx, s.ID, "kill")
}

func signalSessionProcessGroup(s *Session, sig syscall.Signal) error {
	if s == nil || s.Host == nil || s.Host.Cmd == nil || s.Host.Cmd.Process == nil {
		return errNoSessionProcessGroup
	}
	pgid, err := syscall.Getpgid(s.Host.Cmd.Process.Pid)
	if err != nil {
		return err
	}
	if pgid <= 0 {
		return errNoSessionProcessGroup
	}
	return syscall.Kill(-pgid, sig)
}

func budgetWarningForPolicy(p budget.Policy, sessionTokens, dailyTokens, predicted int64) *approval.BudgetWarning {
	var out *approval.BudgetWarning
	if p.Session.MaxTokens > 0 {
		out = tighterBudgetWarning(out, newBudgetWarning("session", sessionTokens, predicted, p.Session.MaxTokens, p.Session.OnOverrun))
	}
	if p.Global.MaxTokensPerDay > 0 {
		out = tighterBudgetWarning(out, newBudgetWarning("daily", dailyTokens, predicted, p.Global.MaxTokensPerDay, p.Session.OnOverrun))
	}
	if out == nil || out.ProjectedTokens <= out.LimitTokens {
		return nil
	}
	return out
}

func tighterBudgetWarning(current, next *approval.BudgetWarning) *approval.BudgetWarning {
	if next == nil {
		return current
	}
	if current == nil || next.RemainingTokens < current.RemainingTokens {
		return next
	}
	return current
}

func newBudgetWarning(scope string, current, predicted, limit int64, action budget.OverrunAction) *approval.BudgetWarning {
	if action == "" {
		action = budget.OverrunInterrupt
	}
	projected := current + predicted
	remaining := limit - current
	message := fmt.Sprintf("Predicted %s budget overrun", scope)
	if predicted == 0 {
		message = fmt.Sprintf("Confirmed %s budget overrun", scope)
	}
	return &approval.BudgetWarning{
		Scope:           scope,
		CurrentTokens:   current,
		PredictedTokens: predicted,
		ProjectedTokens: projected,
		LimitTokens:     limit,
		RemainingTokens: remaining,
		OnOverrun:       string(action),
		Message:         message,
	}
}

func estimateApprovalTokens(ev intake.Event) int64 {
	raw := strings.TrimSpace(ev.InputJSON)
	if raw == "" {
		raw = strings.TrimSpace(ev.RawJSON)
	}
	if raw == "" {
		return 0
	}
	return int64((utf8.RuneCountInString(raw) + 3) / 4)
}

func budgetOverrunKey(sessionID, scope string, limit int64) string {
	return sessionID + ":" + scope + ":" + time.Now().UTC().Format("2006-01-02") + ":" + strconv.FormatInt(limit, 10)
}

func budgetRoot(s *Session, ev intake.Event) string {
	root := strings.TrimSpace(ev.CWD)
	if root == "" && s != nil {
		root = strings.TrimSpace(s.CWD)
	}
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

func claudeCostRef(s *Session, ev intake.Event) budget.SessionRef {
	cwd := strings.TrimSpace(ev.CWD)
	if cwd == "" && s != nil {
		cwd = s.CWD
	}
	sessionID := strings.TrimSpace(ev.Session)
	if sessionID == "" && s != nil {
		sessionID = s.ID
	}
	return budget.SessionRef{
		SessionID:         sessionID,
		ProviderSessionID: strings.TrimSpace(ev.ProviderSessionID),
		Agent:             "claude",
		CWD:               cwd,
	}
}

func (d *Daemon) logClaudeCostError(s *Session, ev intake.Event, err error) {
	if d == nil || err == nil {
		return
	}
	cwd := strings.TrimSpace(ev.CWD)
	if cwd == "" && s != nil {
		cwd = s.CWD
	}
	sessionID := strings.TrimSpace(ev.Session)
	if sessionID == "" && s != nil {
		sessionID = s.ID
	}
	if errors.Is(err, os.ErrNotExist) {
		d.Log.Debug("claude cost transcript not found", slog.String("session", sessionID), slog.String("provider_session", ev.ProviderSessionID), slog.String("cwd", cwd))
		return
	}
	d.Log.Warn("claude cost parse failed", slog.String("session", sessionID), slog.String("provider_session", ev.ProviderSessionID), slog.Any("err", err))
}

func isClaudeAgent(eventAgent, sessionAgent string) bool {
	return strings.EqualFold(strings.TrimSpace(eventAgent), "claude") || strings.EqualFold(strings.TrimSpace(sessionAgent), "claude")
}
