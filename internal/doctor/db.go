package doctor

import (
	"context"

	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
)

func openStoreDB(path string) (*store.DB, error) {
	key, err := secrets.GetStoreKey(context.Background())
	if err != nil {
		return nil, err
	}
	return store.Open(path, store.WithStoreKey(key))
}
