package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadmeDoesNotUseLegacyCommandBlock(t *testing.T) {
	root := filepath.Join("..", "..")
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(readme)
	if strings.Contains(body, beginMark) || strings.Contains(body, endMark) {
		t.Fatal("README still contains legacy command markers")
	}
}
