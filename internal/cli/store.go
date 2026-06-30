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
		return fmt.Errorf("store key write failed; DB rekey rolled back: %w", err)
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"rekeyed": true, "db": paths.DBFile})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Rotated encrypted store key\n", styleFor(cmd).green("[OK]"))
	return nil
}
