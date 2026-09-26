package cli

import (
	"context"
	"os"
	"strings"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/secrets"
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
func telegramToken(paths config.Paths) (string, error) {
	if value := strings.TrimSpace(os.Getenv("ONIBI_TELEGRAM_TOKEN")); value != "" {
		return value, nil
	}
	secretsStore, err := secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return "", err
	}
	value, ok, err := secretsStore.Get(daemon.TelegramSecretBotToken)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(value), nil
}
func telegramSecrets(paths config.Paths) (*secrets.Store, error) {
	return secrets.Open(secrets.Options{EnvFallbackPath: paths.EnvFile})
}
func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
