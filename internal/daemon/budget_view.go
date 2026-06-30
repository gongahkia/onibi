package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
)

func (d *Daemon) handleBudgetRPC(ctx context.Context, _ intake.Event) (intake.Response, error) {
	view, err := d.BudgetView(ctx)
	if err != nil {
		return intake.Response{}, err
	}
	data, err := json.Marshal(view)
	if err != nil {
		return intake.Response{}, err
	}
	return intake.Response{Text: string(data)}, nil
}

func (d *Daemon) BudgetView(ctx context.Context) (budget.View, error) {
	_ = ctx
	now := time.Now().UTC()
	day := now.Format("2006-01-02")
	d.mu.Lock()
	costs := make(map[string]budget.CostEvent, len(d.budgetCosts))
	for id, cost := range d.budgetCosts {
		costs[id] = cost
	}
	dailyTokens := d.budgetDaily[day]
	dailyMicroCents := d.budgetDailyMicroCents[day]
	dailyCostKnown := !d.budgetDailyUnknown[day]
	d.mu.Unlock()
	view := budget.View{Daily: budget.DailyUsage{
		Date:        day,
		TotalTokens: dailyTokens,
		TotalUSD:    microCentsToUSD(dailyMicroCents),
		CostKnown:   dailyCostKnown,
	}}
	var dailyLimit *int64
	seen := map[string]bool{}
	for _, s := range d.Registry.List() {
		cost, hasCost := costs[s.ID]
		if s.Ended() && !hasCost {
			continue
		}
		p, err := policyForBudgetSession(s.CWD)
		if err != nil {
			return budget.View{}, err
		}
		if p.Global.MaxTokensPerDay > 0 && (dailyLimit == nil || p.Global.MaxTokensPerDay < *dailyLimit) {
			dailyLimit = int64Ptr(p.Global.MaxTokensPerDay)
		}
		usage := sessionBudgetUsage(s, cost, p)
		view.Sessions = append(view.Sessions, usage)
		seen[s.ID] = true
	}
	for id, cost := range costs {
		if seen[id] {
			continue
		}
		view.Sessions = append(view.Sessions, costOnlyBudgetUsage(id, cost))
	}
	if dailyLimit != nil {
		view.Daily.LimitTokens = dailyLimit
		remaining := *dailyLimit - view.Daily.TotalTokens
		view.Daily.RemainingTokens = int64Ptr(remaining)
		if view.Daily.CostKnown && view.Daily.TotalTokens > 0 {
			view.Daily.RemainingUSD = float64Ptr(float64(remaining) * view.Daily.TotalUSD / float64(view.Daily.TotalTokens))
		}
	}
	return view, nil
}

func policyForBudgetSession(cwd string) (budget.Policy, error) {
	if strings.TrimSpace(cwd) == "" {
		return budget.DefaultPolicy(), nil
	}
	return budget.LoadPolicy(budget.PolicyPath(cwd))
}

func sessionBudgetUsage(s *Session, cost budget.CostEvent, p budget.Policy) budget.SessionUsage {
	usage := budget.SessionUsage{
		SessionID:         s.ID,
		Name:              s.Name,
		Agent:             s.Agent,
		Model:             cost.Model,
		InputTokens:       cost.InputTokens,
		OutputTokens:      cost.OutputTokens,
		TotalInputTokens:  cost.TotalInputTokens,
		TotalOutputTokens: cost.TotalOutputTokens,
		TotalTokens:       cost.TotalInputTokens + cost.TotalOutputTokens,
		OnOverrun:         string(p.Session.OnOverrun),
	}
	if !cost.TS.IsZero() {
		usage.UpdatedAt = cost.TS.UTC().Format(time.RFC3339Nano)
	}
	if estimate, ok := budget.EstimateCost(cost.Model, cost.TotalInputTokens, cost.TotalOutputTokens); ok {
		usage.CostKnown = true
		usage.TotalUSD = estimate.USD()
	}
	if p.Session.MaxTokens > 0 {
		usage.LimitTokens = int64Ptr(p.Session.MaxTokens)
		remaining := p.Session.MaxTokens - usage.TotalTokens
		usage.RemainingTokens = int64Ptr(remaining)
		if usage.CostKnown && usage.TotalTokens > 0 {
			usage.RemainingUSD = float64Ptr(float64(remaining) * usage.TotalUSD / float64(usage.TotalTokens))
		}
	}
	return usage
}

func costOnlyBudgetUsage(id string, cost budget.CostEvent) budget.SessionUsage {
	usage := budget.SessionUsage{
		SessionID:         id,
		Agent:             cost.Agent,
		Model:             cost.Model,
		InputTokens:       cost.InputTokens,
		OutputTokens:      cost.OutputTokens,
		TotalInputTokens:  cost.TotalInputTokens,
		TotalOutputTokens: cost.TotalOutputTokens,
		TotalTokens:       cost.TotalInputTokens + cost.TotalOutputTokens,
	}
	if !cost.TS.IsZero() {
		usage.UpdatedAt = cost.TS.UTC().Format(time.RFC3339Nano)
	}
	if estimate, ok := budget.EstimateCost(cost.Model, cost.TotalInputTokens, cost.TotalOutputTokens); ok {
		usage.CostKnown = true
		usage.TotalUSD = estimate.USD()
	}
	return usage
}

func microCentsToUSD(v int64) float64 {
	return float64(v) / float64(budget.MicroCentsPerCent) / 100
}

func int64Ptr(v int64) *int64 {
	return &v
}

func float64Ptr(v float64) *float64 {
	return &v
}
