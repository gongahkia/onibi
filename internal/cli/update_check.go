package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

const updateCheckTimeout = 2 * time.Second

func runUpdateCheck(cmd *cobra.Command, _ []string) error {
	repo, _ := cmd.Flags().GetString("repo")
	noGitHub, _ := cmd.Flags().GetBool("no-github")
	asJSON, _ := cmd.Flags().GetBool("json")
	ctx, cancel := context.WithTimeout(cmd.Context(), updateCheckTimeout)
	defer cancel()
	res := updatecheck.Check(ctx, updatecheck.Options{
		CurrentVersion: buildinfo.Version,
		CurrentCommit:  buildinfo.Commit,
		RepoDir:        repo,
		CheckGitHub:    !noGitHub,
		Timeout:        updateCheckTimeout,
	})
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	switch res.Status {
	case updatecheck.StatusOutdated:
		fmt.Fprintf(cmd.OutOrStdout(), "update available: %s\n", res.Detail)
	case updatecheck.StatusCurrent:
		fmt.Fprintf(cmd.OutOrStdout(), "up to date: %s\n", res.Detail)
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "update check unavailable: %s\n", res.Detail)
	}
	if res.Command != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "next: %s\n", res.Command)
	}
	return nil
}
