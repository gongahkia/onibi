package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadmeTelegramCommandsMatchHelpText(t *testing.T) {
	root := filepath.Join("..", "..")
	commands, err := readHelpCommands(filepath.Join(root, "internal", "daemon", "commands.go"))
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	block := generatedBlock(commands)
	if !strings.Contains(string(readme), block) {
		t.Fatal("README Telegram command block is stale")
	}
}
