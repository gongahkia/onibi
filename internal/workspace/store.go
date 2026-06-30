package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/store"
	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultWorkspaceKVKey = "workspace:default"
	indexDirPerm          = 0o700
	indexFilePerm         = 0o600
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type IndexEntry struct {
	Name             string    `toml:"-"`
	Path             string    `toml:"path"`
	LastSeen         time.Time `toml:"last_seen"`
	SSHKey           string    `toml:"ssh_key,omitempty"`
	DefaultTransport string    `toml:"default_transport,omitempty"`
}

type DBEntry struct {
	Name      string
	Path      string
	SSHKeyRef string
	LastSeen  time.Time
}

type DBStore struct {
	db *store.DB
}

func DefaultIndexDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".onibi", "workspaces"), nil
}

func EntryPath(dir, name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".toml"), nil
}

func SaveIndexEntry(dir string, entry IndexEntry) error {
	entry, err := normalizeIndexEntry(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, indexDirPerm); err != nil {
		return fmt.Errorf("mkdir workspace index: %w", err)
	}
	path, err := EntryPath(dir, entry.Name)
	if err != nil {
		return err
	}
	data, err := toml.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal workspace index: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+entry.Name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create workspace index temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(indexFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod workspace index temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write workspace index temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close workspace index temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace workspace index: %w", err)
	}
	if err := os.Chmod(path, indexFilePerm); err != nil {
		return fmt.Errorf("chmod workspace index: %w", err)
	}
	return nil
}

func LoadIndexEntry(dir, name string) (IndexEntry, error) {
	path, err := EntryPath(dir, name)
	if err != nil {
		return IndexEntry{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return IndexEntry{}, fmt.Errorf("read workspace index: %w", err)
	}
	var entry IndexEntry
	if err := toml.Unmarshal(data, &entry); err != nil {
		return IndexEntry{}, fmt.Errorf("parse workspace index: %w", err)
	}
	entry.Name = name
	return normalizeIndexEntry(entry)
}

func NewDBStore(db *store.DB) (*DBStore, error) {
	if db == nil {
		return nil, errors.New("workspace db required")
	}
	if db.CryptBox() == nil {
		return nil, store.ErrCryptBoxUnavailable
	}
	return &DBStore{db: db}, nil
}

func SetDefaultName(ctx context.Context, db *store.DB, name string) error {
	if db == nil {
		return errors.New("workspace db required")
	}
	if err := validateName(name); err != nil {
		return err
	}
	return db.KVSetString(ctx, DefaultWorkspaceKVKey, name)
}

func DefaultName(ctx context.Context, db *store.DB) (string, bool, error) {
	if db == nil {
		return "", false, errors.New("workspace db required")
	}
	name, ok, err := db.KVGetString(ctx, DefaultWorkspaceKVKey)
	if err != nil || !ok {
		return "", ok, err
	}
	if err := validateName(name); err != nil {
		return "", false, err
	}
	return name, true, nil
}

func ClearDefaultName(ctx context.Context, db *store.DB, name string) error {
	if db == nil {
		return errors.New("workspace db required")
	}
	current, ok, err := DefaultName(ctx, db)
	if err != nil || !ok {
		return err
	}
	if name == "" || current == name {
		return db.KVDel(ctx, DefaultWorkspaceKVKey)
	}
	return nil
}

func (s *DBStore) Upsert(ctx context.Context, entry DBEntry) error {
	entry, err := normalizeDBEntry(entry)
	if err != nil {
		return err
	}
	pathEnc, err := s.sealPath(ctx, entry.Name, entry.Path)
	if err != nil {
		return err
	}
	_, err = s.db.SQL().ExecContext(ctx,
		`INSERT INTO workspaces(name, path_enc, ssh_key_ref, last_seen)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   path_enc=excluded.path_enc,
		   ssh_key_ref=excluded.ssh_key_ref,
		   last_seen=excluded.last_seen`,
		entry.Name, pathEnc, nullIfEmpty(entry.SSHKeyRef), entry.LastSeen.Unix())
	return err
}

func (s *DBStore) Get(ctx context.Context, name string) (DBEntry, bool, error) {
	if err := validateName(name); err != nil {
		return DBEntry{}, false, err
	}
	row := s.db.SQL().QueryRowContext(ctx,
		`SELECT name, path_enc, COALESCE(ssh_key_ref, ''), last_seen
		   FROM workspaces WHERE name = ?`, name)
	entry, err := s.scanRow(ctx, row)
	if errors.Is(err, sql.ErrNoRows) {
		return DBEntry{}, false, nil
	}
	if err != nil {
		return DBEntry{}, false, err
	}
	return entry, true, nil
}

func (s *DBStore) List(ctx context.Context) ([]DBEntry, error) {
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT name, path_enc, COALESCE(ssh_key_ref, ''), last_seen
		   FROM workspaces ORDER BY last_seen DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DBEntry
	for rows.Next() {
		entry, err := s.scanRow(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *DBStore) Remove(ctx context.Context, name string) (bool, error) {
	if err := validateName(name); err != nil {
		return false, err
	}
	res, err := s.db.SQL().ExecContext(ctx, `DELETE FROM workspaces WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

type workspaceScanner interface {
	Scan(dest ...any) error
}

func (s *DBStore) scanRow(ctx context.Context, row workspaceScanner) (DBEntry, error) {
	var entry DBEntry
	var pathEnc []byte
	var lastSeen int64
	if err := row.Scan(&entry.Name, &pathEnc, &entry.SSHKeyRef, &lastSeen); err != nil {
		return DBEntry{}, err
	}
	path, err := s.openPath(ctx, entry.Name, pathEnc)
	if err != nil {
		return DBEntry{}, err
	}
	entry.Path = path
	entry.LastSeen = time.Unix(lastSeen, 0).UTC()
	return entry, nil
}

func (s *DBStore) sealPath(ctx context.Context, name, path string) ([]byte, error) {
	return s.db.CryptBox().Seal(ctx, []byte(path), store.RowAAD("workspaces", name, "path_enc"))
}

func (s *DBStore) openPath(ctx context.Context, name string, sealed []byte) (string, error) {
	opened, err := s.db.CryptBox().Open(ctx, sealed, store.RowAAD("workspaces", name, "path_enc"))
	if err != nil {
		return "", err
	}
	return string(opened), nil
}

func normalizeIndexEntry(entry IndexEntry) (IndexEntry, error) {
	if err := validateName(entry.Name); err != nil {
		return IndexEntry{}, err
	}
	if entry.Path == "" {
		return IndexEntry{}, errors.New("workspace path required")
	}
	if entry.LastSeen.IsZero() {
		entry.LastSeen = time.Now().UTC()
	}
	entry.SSHKey = strings.TrimSpace(entry.SSHKey)
	entry.DefaultTransport = strings.ToLower(strings.TrimSpace(entry.DefaultTransport))
	if entry.DefaultTransport != "" {
		if err := validateTransportMode(entry.DefaultTransport); err != nil {
			return IndexEntry{}, fmt.Errorf("default_transport: %w", err)
		}
	}
	return entry, nil
}

func normalizeDBEntry(entry DBEntry) (DBEntry, error) {
	if err := validateName(entry.Name); err != nil {
		return DBEntry{}, err
	}
	if entry.Path == "" {
		return DBEntry{}, errors.New("workspace path required")
	}
	if entry.LastSeen.IsZero() {
		entry.LastSeen = time.Now().UTC()
	}
	return entry, nil
}

func validateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid workspace name %q", name)
	}
	return nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
