package secrets

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/envelope"

	"github.com/99designs/keyring"
)

// Backend names a secret-storage backend.
type Backend string

const (
	BackendKeychain Backend = "keychain" // macOS Keychain
	BackendSecret   Backend = "secret"   // Linux Secret Service
	BackendDotenv   Backend = "dotenv"   // .env file fallback (0600)
)

const (
	keyringService = "sh.onibi.daemon"
	StoreKeyName   = "onibi.store.key.v1"
)

// Store hides whether a secret lives in the OS keystore or a .env file.
// Open returns one of these wired to the right backend.
type Store struct {
	backend Backend
	ring    keyring.Keyring // nil for dotenv
	envPath string          // only set for dotenv
}

// Options configures Open.
type Options struct {
	// EnvFallbackPath is consulted when no keystore is available. Required.
	EnvFallbackPath string
	// PreferDotenv forces the .env backend regardless of OS keystore
	// availability. Off by default; useful for testing.
	PreferDotenv bool
}

// Open returns a Store using the OS keystore where available, else the .env
// fallback at opts.EnvFallbackPath (created on first write with 0600 perms).
func Open(opts Options) (*Store, error) {
	if opts.EnvFallbackPath == "" {
		return nil, errors.New("EnvFallbackPath required")
	}
	if opts.PreferDotenv {
		return &Store{backend: BackendDotenv, envPath: opts.EnvFallbackPath}, nil
	}
	ring, err := openKeyring()
	if err != nil {
		// graceful degradation to dotenv
		return &Store{backend: BackendDotenv, envPath: opts.EnvFallbackPath}, nil
	}
	be := BackendSecret
	if runtime.GOOS == "darwin" {
		be = BackendKeychain
	}
	return &Store{backend: be, ring: ring}, nil
}

func DefaultStoreKeyFallbackPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "onibi", "store.key"), nil
}

func GetOrCreateStoreKey(ctx context.Context) ([]byte, error) {
	path, err := DefaultStoreKeyFallbackPath()
	if err != nil {
		return nil, err
	}
	store, err := Open(Options{EnvFallbackPath: path, PreferDotenv: forceDotenvStoreKey()})
	if err != nil {
		return nil, err
	}
	return store.GetOrCreateStoreKey(ctx)
}

func forceDotenvStoreKey() bool {
	if strings.EqualFold(os.Getenv("ONIBI_STORE_KEY_BACKEND"), "dotenv") {
		return true
	}
	return strings.HasSuffix(filepath.Base(os.Args[0]), ".test")
}

func openKeyring() (keyring.Keyring, error) {
	return keyring.Open(keyring.Config{
		ServiceName: keyringService,
		// macOS: use the dedicated onibi keychain item; KeychainAccessibleWhenUnlocked
		// means the daemon can read after first login unlock without per-call prompts.
		KeychainTrustApplication:       true,
		KeychainAccessibleWhenUnlocked: true,
		// Linux: only enable kwallet and secret-service; LibSecret is the modern default.
		AllowedBackends: []keyring.BackendType{
			keyring.KeychainBackend,
			keyring.SecretServiceBackend,
			keyring.WinCredBackend,
		},
	})
}

// Backend returns the active backend (informational, e.g. for `onibi doctor`).
func (s *Store) Backend() Backend { return s.backend }

func (s *Store) GetOrCreateStoreKey(ctx context.Context) ([]byte, error) {
	value, ok, err := s.getContext(ctx, StoreKeyName)
	if err != nil {
		return nil, err
	}
	if ok {
		return decodeStoreKey(value)
	}
	key := make([]byte, envelope.KeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := s.setContext(ctx, StoreKeyName, base64.RawURLEncoding.EncodeToString(key)); err != nil {
		return nil, err
	}
	value, ok, err = s.getContext(ctx, StoreKeyName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("store key write did not persist")
	}
	return decodeStoreKey(value)
}

// Set stores value under key. For .env, writes the file atomically with
// 0600 perms (creates if missing).
func (s *Store) Set(key, value string) error {
	if s.backend == BackendDotenv {
		return setDotenv(s.envPath, key, value)
	}
	return s.ring.Set(keyring.Item{
		Key:         key,
		Data:        []byte(value),
		Label:       "Onibi — " + key,
		Description: "Onibi daemon credential",
	})
}

// Get retrieves key, returning ("", false, nil) if missing.
func (s *Store) Get(key string) (string, bool, error) {
	if s.backend == BackendDotenv {
		v, ok, err := getDotenv(s.envPath, key)
		return v, ok, err
	}
	it, err := s.ring.Get(key)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(it.Data), true, nil
}

func (s *Store) getContext(ctx context.Context, key string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if s.backend == BackendDotenv {
		value, ok, err := s.Get(key)
		if err != nil {
			return "", false, err
		}
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		return value, ok, nil
	}
	type result struct {
		value string
		ok    bool
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		value, ok, err := s.Get(key)
		ch <- result{value: value, ok: ok, err: err}
	}()
	select {
	case res := <-ch:
		return res.value, res.ok, res.err
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

func (s *Store) setContext(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.backend == BackendDotenv {
		if err := s.Set(key, value); err != nil {
			return err
		}
		return ctx.Err()
	}
	ch := make(chan error, 1)
	go func() {
		ch <- s.Set(key, value)
	}()
	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetWithTimeout retrieves key, bounding OS keystore calls that can block.
func (s *Store) GetWithTimeout(ctx context.Context, key string, timeout time.Duration) (string, bool, error) {
	if s.backend == BackendDotenv || timeout <= 0 {
		return s.Get(key)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		value string
		ok    bool
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		value, ok, err := s.Get(key)
		ch <- result{value: value, ok: ok, err: err}
	}()
	select {
	case res := <-ch:
		return res.value, res.ok, res.err
	case <-ctx.Done():
		return "", false, fmt.Errorf("secret %s lookup timeout: %w", key, ctx.Err())
	}
}

// Delete removes key (no-op if missing).
func (s *Store) Delete(key string) error {
	if s.backend == BackendDotenv {
		return delDotenv(s.envPath, key)
	}
	err := s.ring.Remove(key)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	return err
}

// ----------------------------------------------------------------------------
// .env fallback (KEY="quoted-value" lines, 0600)
// ----------------------------------------------------------------------------

func setDotenv(path, key, value string) error {
	entries, _ := readDotenv(path) // ok if missing
	entries[key] = value
	return writeDotenv(path, entries)
}

func getDotenv(path, key string) (string, bool, error) {
	entries, err := readDotenv(path)
	if err != nil {
		return "", false, err
	}
	v, ok := entries[key]
	return v, ok, nil
}

func delDotenv(path, key string) error {
	entries, err := readDotenv(path)
	if err != nil {
		return err
	}
	if _, ok := entries[key]; !ok {
		return nil
	}
	delete(entries, key)
	return writeDotenv(path, entries)
}

func readDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := checkPerm(f, 0o600); err != nil {
		return nil, err
	}
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 1 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"`)
		out[k] = v
	}
	return out, sc.Err()
}

func writeDotenv(path string, entries map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	_, _ = io.WriteString(w, "# onibi secrets — do not commit; 0600 enforced\n")
	for k, v := range entries {
		fmt.Fprintf(w, "%s=%q\n", k, v)
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func decodeStoreKey(value string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	if len(key) != envelope.KeyBytes {
		return nil, fmt.Errorf("store key must be %d bytes", envelope.KeyBytes)
	}
	return key, nil
}

func checkPerm(f *os.File, want os.FileMode) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if got := fi.Mode().Perm(); got != want {
		return fmt.Errorf("%s has perms %#o (want %#o)", f.Name(), got, want)
	}
	return nil
}
