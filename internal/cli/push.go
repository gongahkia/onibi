package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/web"
)

func pushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Maintain Web Push state",
	}
	rotate := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate VAPID keys and invalidate subscriptions",
		RunE:  runPushRotate,
	}
	rotate.Flags().Bool("json", false, "print JSON")
	cmd.AddCommand(rotate)
	return cmd
}

func runPushRotate(cmd *cobra.Command, _ []string) error {
	_, db, err := openCLIStore()
	if err != nil {
		return err
	}
	defer db.Close()
	keys, invalidated, err := web.RotateVAPIDKeys(cmd.Context(), db)
	if err != nil {
		return err
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"rotated":                   true,
			"public_key":                keys.PublicKey,
			"subscriptions_invalidated": invalidated,
			"resubscribe_command":       "open installed app and tap PUSH",
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Rotated Web Push VAPID key\n", styleFor(cmd).green("[OK]"))
	fmt.Fprintf(cmd.OutOrStdout(), "%s Invalidated %d push subscription(s); open the installed app and tap PUSH to re-subscribe\n", styleFor(cmd).yellow("[WARN]"), invalidated)
	return nil
}
