package cli

import (
	"context"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func pathsAndStore() (config.Paths, *store.DB, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Paths{}, nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return config.Paths{}, nil, err
	}
	db, err := store.Open(paths.DBFile)
	return paths, db, err
}
func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
