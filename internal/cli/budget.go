package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/intake"
)

var budgetRPCRequest = func(ctx context.Context, ev intake.Event) (intake.Response, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return intake.Response{}, err
	}
	resp, err := intake.Request(paths.Socket, ev, 30*time.Second)
	if err != nil {
		return intake.Response{}, fmt.Errorf("onibi service unavailable; run `onibi up`: %w", err)
	}
	if strings.TrimSpace(resp.Reason) != "" {
		return resp, errors.New(resp.Reason)
	}
	return resp, nil
}

func budgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Show token budget usage",
		Args:  cobra.ExactArgs(0),
		RunE:  runBudgetShow,
	}
	cmd.Flags().Bool("json", false, "print JSON")
	show := &cobra.Command{
		Use:   "show",
		Short: "Show token budget usage",
		Args:  cobra.ExactArgs(0),
		RunE:  runBudgetShow,
	}
	show.Flags().Bool("json", false, "print JSON")
	cmd.AddCommand(show)
	return cmd
}

func runBudgetShow(cmd *cobra.Command, _ []string) error {
	resp, err := budgetRPCRequest(cmd.Context(), intake.Event{Type: intake.TypeBudget})
	if err != nil {
		return err
	}
	var view budget.View
	if err := json.Unmarshal([]byte(resp.Text), &view); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	return renderBudgetView(cmd, view)
}

func renderBudgetView(cmd *cobra.Command, view budget.View) error {
	style := styleFor(cmd)
	fmt.Fprintf(cmd.OutOrStdout(), "Daily %s: %s tokens, %s spent, %s remaining\n",
		view.Daily.Date,
		formatInt64(view.Daily.TotalTokens),
		formatBudgetUSD(view.Daily.TotalUSD, view.Daily.CostKnown),
		formatBudgetRemaining(view.Daily.RemainingTokens, view.Daily.RemainingUSD, view.Daily.CostKnown),
	)
	table := [][]string{tableHeader(style, "SESSION", "AGENT", "MODEL", "TOKENS", "USD", "LIMIT", "REMAINING", "OVERRUN")}
	for _, s := range view.Sessions {
		table = append(table, []string{
			sessionBudgetName(s),
			emptyDash(s.Agent),
			emptyDash(s.Model),
			formatInt64(s.TotalTokens),
			formatBudgetUSD(s.TotalUSD, s.CostKnown),
			formatBudgetLimit(s.LimitTokens),
			formatBudgetRemaining(s.RemainingTokens, s.RemainingUSD, s.CostKnown),
			emptyDash(s.OnOverrun),
		})
	}
	if len(table) == 1 {
		fmt.Fprintln(cmd.OutOrStdout(), style.dim("no live budget sessions"))
		return nil
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func sessionBudgetName(s budget.SessionUsage) string {
	if strings.TrimSpace(s.Name) != "" {
		return s.Name + " (" + s.SessionID + ")"
	}
	return emptyDash(s.SessionID)
}

func formatBudgetLimit(v *int64) string {
	if v == nil {
		return "unlimited"
	}
	return formatInt64(*v)
}

func formatBudgetRemaining(tokens *int64, usd *float64, known bool) string {
	if tokens == nil {
		return "unlimited"
	}
	out := formatInt64(*tokens) + " tokens"
	if known && usd != nil {
		out += " / " + formatUSD(*usd)
	}
	return out
}

func formatBudgetUSD(v float64, known bool) string {
	if !known {
		return "n/a"
	}
	return formatUSD(v)
}

func formatUSD(v float64) string {
	if v > -1 && v < 1 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func formatInt64(v int64) string {
	return fmt.Sprintf("%d", v)
}

func emptyDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}
