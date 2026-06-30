package cli

import (
	"context"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

func openDefaultDB() (*store.DB, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}
	key, err := secrets.GetOrCreateStoreKey(context.Background())
	if err != nil {
		return nil, err
	}
	return store.Open(paths.DBFile, store.WithStoreKey(key))
}
