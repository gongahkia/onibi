package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	store := &Store{db: db}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure sqlite database: %w", err)
	}
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) Ping(ctx context.Context) error {
	if store == nil || store.db == nil {
		return errors.New("database is not open")
	}
	return store.db.PingContext(ctx)
}

func (store *Store) Now(ctx context.Context) (time.Time, error) {
	var value string
	if err := store.db.QueryRowContext(ctx, "SELECT strftime('%Y-%m-%dT%H:%M:%fZ', 'now')").Scan(&value); err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse database time: %w", err)
	}
	return parsed, nil
}

func (store *Store) DB() *sql.DB {
	return store.db
}
