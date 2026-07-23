package cli

import (
	"context"
	"fmt"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/spf13/cobra"
)

func openDefaultDB() (*store.DB, error) {
	return openDefaultDBForCommand(nil)
}

func openDefaultDBForCommand(cmd *cobra.Command) (*store.DB, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}
	secretStore, err := openDefaultSecretStoreForCommand(cmd)
	if err != nil {
		return nil, err
	}
	key, err := secretStore.GetOrCreateStoreKey(context.Background())
	if err != nil {
		return nil, err
	}
	return store.Open(paths.DBFile, store.WithStoreKey(key))
}

func openDefaultSecretStoreForCommand(cmd *cobra.Command) (*secrets.Store, error) {
	secretStore, err := secrets.OpenDefault()
	if err != nil {
		return nil, err
	}
	printKeychainPreflight(cmd, secretStore.Backend())
	return secretStore, nil
}

func printKeychainPreflight(cmd *cobra.Command, backend secrets.Backend) {
	if cmd == nil || backend != secrets.BackendKeychain {
		return
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "macOS may request Keychain access for \"Onibi — onibi.store.key.v1\". Enter your login keychain password in the macOS dialog and choose Allow; never enter it in this terminal.")
}
