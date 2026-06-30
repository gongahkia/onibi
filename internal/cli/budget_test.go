package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
)

func TestBudgetCLIShowJSON(t *testing.T) {
	remainingTokens := int64(87)
	remainingUSD := 0.000522
	withBudgetRPC(t, func(_ context.Context, ev intake.Event) (intake.Response, error) {
		if ev.Type != intake.TypeBudget {
			t.Fatalf("event = %#v", ev)
		}
		data, err := json.Marshal(budget.View{
			Sessions: []budget.SessionUsage{{
				SessionID:       "s1",
				Name:            "work",
				Agent:           "claude",
				Model:           "claude-sonnet-4-6",
				TotalTokens:     13,
				TotalUSD:        0.000078,
				CostKnown:       true,
				LimitTokens:     int64p(100),
				RemainingTokens: &remainingTokens,
				RemainingUSD:    &remainingUSD,
				OnOverrun:       "interrupt",
			}},
			Daily: budget.DailyUsage{
				Date:            "2026-06-30",
				TotalTokens:     13,
				TotalUSD:        0.000078,
				CostKnown:       true,
				LimitTokens:     int64p(1000),
				RemainingTokens: int64p(987),
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return intake.Response{Text: string(data)}, nil
	})
	out, _ := executeRoot(t, "budget", "show", "--json", "--color", "never")
	var view budget.View
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if view.Daily.TotalTokens != 13 || len(view.Sessions) != 1 || view.Sessions[0].RemainingTokens == nil || *view.Sessions[0].RemainingTokens != 87 {
		t.Fatalf("view = %#v", view)
	}
}

func TestBudgetCLIShowRendersHumanSummary(t *testing.T) {
	withBudgetRPC(t, func(_ context.Context, _ intake.Event) (intake.Response, error) {
		data, err := json.Marshal(budget.View{
			Sessions: []budget.SessionUsage{{
				SessionID:       "s1",
				Agent:           "claude",
				Model:           "claude-sonnet-4-6",
				TotalTokens:     13,
				TotalUSD:        0.000078,
				CostKnown:       true,
				LimitTokens:     int64p(100),
				RemainingTokens: int64p(87),
				OnOverrun:       "interrupt",
			}},
			Daily: budget.DailyUsage{Date: "2026-06-30", TotalTokens: 13, CostKnown: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		return intake.Response{Text: string(data)}, nil
	})
	out, _ := executeRoot(t, "budget", "--color", "never")
	got := out.String()
	for _, want := range []string{"Daily 2026-06-30", "s1", "claude-sonnet-4-6", "87 tokens"} {
		if !strings.Contains(got, want) {
			t.Fatalf("out = %q, want %q", got, want)
		}
	}
}

func withBudgetRPC(t *testing.T, fn func(context.Context, intake.Event) (intake.Response, error)) {
	t.Helper()
	old := budgetRPCRequest
	budgetRPCRequest = fn
	t.Cleanup(func() { budgetRPCRequest = old })
}

func int64p(v int64) *int64 {
	return &v
}
