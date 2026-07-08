package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	stored "github.com/gongahkia/onibi/internal/store"
)

func storeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Maintain encrypted local state",
	}
	rekey := &cobra.Command{
		Use:   "rekey",
		Short: "Rotate the encrypted SQLite state master key",
		RunE:  runStoreRekey,
	}
	rekey.Flags().Bool("json", false, "print JSON")
	rekey.Flags().Bool("dry-run", false, "report rekey impact without rotating")
	cmd.AddCommand(rekey)
	return cmd
}

func runStoreRekey(cmd *cobra.Command, _ []string) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	oldKey, err := secrets.GetOrCreateStoreKey(cmd.Context())
	if err != nil {
		return err
	}
	db, err := stored.Open(paths.DBFile, stored.WithStoreKey(oldKey))
	if err != nil {
		return err
	}
	defer db.Close()
	impact, err := db.RekeyImpact(cmd.Context())
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"dry_run":                    true,
				"db":                         paths.DBFile,
				"active_web_sessions":        impact.ActiveWebSessions,
				"web_sessions_to_revoke":     impact.WebSessionsToRevoke,
				"web_sessions_to_reseal":     impact.WebSessionsToReseal,
				"pairing_tokens_to_reseal":   impact.PairingTokensToReseal,
				"websocket_close_reason":     stored.WebSessionReasonStoreRekey,
				"store_key_would_be_rotated": true,
			})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Dry run: store rekey would rotate encrypted store key\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Active web sessions: %d\n", impact.ActiveWebSessions)
		fmt.Fprintf(cmd.OutOrStdout(), "Web sessions to revoke: %d\n", impact.WebSessionsToRevoke)
		fmt.Fprintf(cmd.OutOrStdout(), "Web sessions to re-seal: %d\n", impact.WebSessionsToReseal)
		fmt.Fprintf(cmd.OutOrStdout(), "Pairing tokens to re-seal: %d\n", impact.PairingTokensToReseal)
		fmt.Fprintf(cmd.OutOrStdout(), "Open WebSockets close with reason %q on the next validity check.\n", stored.WebSessionReasonStoreRekey)
		return nil
	}
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return err
	}
	if err := db.Rekey(cmd.Context(), newKey); err != nil {
		return err
	}
	if err := secrets.SetStoreKey(cmd.Context(), newKey); err != nil {
		rollbackErr := db.Rekey(cmd.Context(), oldKey)
		if rollbackErr != nil {
			return fmt.Errorf("store key write failed after DB rekey; rollback failed too: write=%w rollback=%v", err, rollbackErr)
		}
		return fmt.Errorf("store key write failed; DB key rolled back; web session revocations remain: %w", err)
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"rekeyed":                 true,
			"db":                      paths.DBFile,
			"active_web_sessions":     impact.ActiveWebSessions,
			"web_sessions_revoked":    impact.WebSessionsToRevoke,
			"web_sessions_resealed":   impact.WebSessionsToReseal,
			"pairing_tokens_resealed": impact.PairingTokensToReseal,
			"websocket_close_reason":  stored.WebSessionReasonStoreRekey,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Rotated encrypted store key\n", styleFor(cmd).green("[OK]"))
	if impact.WebSessionsToRevoke > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "%s Revoked %d web session(s); open WebSockets close with reason %q on the next validity check\n", styleFor(cmd).yellow("[WARN]"), impact.WebSessionsToRevoke, stored.WebSessionReasonStoreRekey)
	}
	return nil
}
