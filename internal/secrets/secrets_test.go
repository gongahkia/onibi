package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDotenvSetPreservesUnreadableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := []byte("EXISTING=keep\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := Open(Options{EnvFallbackPath: path, PreferDotenv: true})
	if err := s.Set("NEW", "secret"); err == nil {
		t.Fatal("Set must fail when existing secrets cannot be safely read")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("existing secrets changed: %q, %v", got, err)
	}
}

func TestDotenvSetDoesNotReusePredictableTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	target := path + ".tmp"
	if err := os.WriteFile(target, []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := Open(Options{EnvFallbackPath: path, PreferDotenv: true})
	if err := s.Set("TOKEN", "secret"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file must be private: %v, %v", info, err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "unrelated" {
		t.Fatalf("unrelated temporary file changed: %q, %v", got, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".onibi-secrets-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary secrets left behind: %v, %v", leftovers, err)
	}
}

func TestDotenvRoundtrip(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	s, err := Open(Options{EnvFallbackPath: envFile, PreferDotenv: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend() != BackendDotenv {
		t.Fatalf("expected dotenv backend, got %s", s.Backend())
	}
	if err := s.Set("TEST_SECRET", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := s.Get("TEST_SECRET")
	if err != nil || !ok || got != "value" {
		t.Fatalf("Get: %q ok=%v err=%v", got, ok, err)
	}
	// perms must be 0600
	fi, err := os.Stat(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("env file perms %#o (want 0600)", fi.Mode().Perm())
	}
}

func TestDotenvRoundtripEscapedJSON(t *testing.T) {
	s, err := Open(Options{EnvFallbackPath: filepath.Join(t.TempDir(), ".env"), PreferDotenv: true})
	if err != nil {
		t.Fatal(err)
	}
	value := `{"private_key":"a/b+c","public_key":"x_y-z"}`
	if err := s.Set("JSON_SECRET", value); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("JSON_SECRET")
	if err != nil || !ok || got != value {
		t.Fatalf("Get: %q ok=%v err=%v", got, ok, err)
	}
}

func TestDotenvDelete(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	s, _ := Open(Options{EnvFallbackPath: envFile, PreferDotenv: true})
	_ = s.Set("FOO", "bar")
	if err := s.Delete("FOO"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ := s.Get("FOO")
	if ok {
		t.Fatal("expected delete to remove key")
	}
}

func TestDotenvMissingKey(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	s, _ := Open(Options{EnvFallbackPath: envFile, PreferDotenv: true})
	_, ok, err := s.Get("missing")
	if err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
}

func TestDotenvLoosePermsRejected(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	// create file with loose perms
	f, _ := os.OpenFile(envFile, os.O_CREATE|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("FOO=bar\n")
	_ = f.Close()

	s, _ := Open(Options{EnvFallbackPath: envFile, PreferDotenv: true})
	_, _, err := s.Get("FOO")
	if err == nil {
		t.Fatal("expected error on loose perms")
	}
}
