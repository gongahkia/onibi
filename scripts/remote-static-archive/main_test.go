package main

import (
	"archive/zip"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCreatesDeterministicArchive(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{
		"dist/index.html":    "<!doctype html>",
		"dist/assets/app.js": "console.log('onibi')",
		"fonts/Onibi.woff2":  "font",
	} {
		file := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(root, "dist-remote.zip")
	if err := run(root, output); err != nil {
		t.Fatal(err)
	}
	first := digest(t, output)
	if err := run(root, output); err != nil {
		t.Fatal(err)
	}
	if got := digest(t, output); got != first {
		t.Fatalf("archive changed: %x != %x", got, first)
	}
	archive, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, name := range []string{"dist/index.html", "dist/assets/app.js", "fonts/Onibi.woff2"} {
		found := false
		for _, file := range archive.File {
			if file.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s", name)
		}
	}
}

func digest(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	var sum [sha256.Size]byte
	copy(sum[:], hash.Sum(nil))
	return sum
}
