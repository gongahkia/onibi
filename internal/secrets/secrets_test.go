package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

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
