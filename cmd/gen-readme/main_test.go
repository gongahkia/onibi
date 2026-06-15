package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/telegram"
)

func TestReadmeTelegramCommandsMatchHelpText(t *testing.T) {
	root := filepath.Join("..", "..")
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	block := generatedBlock(telegram.CommandLinesForReadme())
	if !strings.Contains(string(readme), block) {
		t.Fatal("README Telegram command block is stale")
	}
}
