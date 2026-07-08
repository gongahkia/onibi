//go:build onibi_remote

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/envelope"
)

func TestRemoteStoreKeyCreatesAndReusesKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.key")
	first, err := remoteStoreKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != envelope.KeyBytes {
		t.Fatalf("key len = %d", len(first))
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("perms = %#o", got)
	}
	second, err := remoteStoreKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("key changed")
	}
}

func TestRemoteStoreKeyRejectsLoosePerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.key")
	if err := os.WriteFile(path, []byte(remoteStoreKeyName+"=\"bad\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteStoreKey(path); err == nil {
		t.Fatal("expected loose perms error")
	}
}
