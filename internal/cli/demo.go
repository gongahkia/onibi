package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/intake"
)

func runDemoApproval(cmd *cobra.Command, _ []string) error {
	tool, _ := cmd.Flags().GetString("tool")
	inputJSON, _ := cmd.Flags().GetString("input")
	cwd, _ := os.Getwd()
	resp, err := sessionRPC(cmd.Context(), intake.Event{
		Type:      intake.TypeDemoApproval,
		Agent:     "demo",
		Tool:      strings.TrimSpace(tool),
		InputJSON: strings.TrimSpace(inputJSON),
		CWD:       cwd,
	})
	if err != nil {
		return err
	}
	if resp.Reason != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "demo approval: %s (%s)\n", resp.Decision, resp.Reason)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "demo approval: %s\n", resp.Decision)
	return nil
}
