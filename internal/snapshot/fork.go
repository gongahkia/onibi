package snapshot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Fork(s Snapshot, atTurn int, newPrompt string) (Session, error) {
	return ForkContext(context.Background(), s, atTurn, newPrompt, RestoreOptions{})
}

func ForkContext(ctx context.Context, s Snapshot, atTurn int, newPrompt string, opts RestoreOptions) (Session, error) {
	if atTurn < 0 {
		return Session{}, errors.New("snapshot: atTurn must be >= 0")
	}
	prompt := strings.TrimSpace(newPrompt)
	if prompt == "" {
		return Session{}, errors.New("snapshot: new prompt required")
	}
	if strings.TrimSpace(s.Transcript) == "" {
		return Session{}, errors.New("snapshot: transcript required")
	}
	offset, err := rewriteTranscriptForFork(s.Transcript, atTurn, prompt)
	if err != nil {
		return Session{}, err
	}
	s.TranscriptOffset = offset
	return RestoreContext(ctx, s, opts)
}

func rewriteTranscriptForFork(path string, atTurn int, prompt string) (int64, error) {
	lines, err := readJSONLLines(path)
	if err != nil {
		return 0, err
	}
	if atTurn < len(lines) {
		lines = lines[:atTurn]
	}
	injected, err := forkPromptLine(prompt)
	if err != nil {
		return 0, err
	}
	lines = append(lines, injected)
	tmp := path + ".onibi-fork.tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	w := bufio.NewWriter(f)
	var offset int64
	for _, line := range lines {
		n, err := w.Write(append(line, '\n'))
		offset += int64(n)
		if err != nil {
			_ = f.Close()
			return 0, err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	return offset, nil
}

func readJSONLLines(path string) ([][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var lines [][]byte
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, []byte(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func forkPromptLine(prompt string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
		"onibi": map[string]any{
			"forked": true,
			"ts":     time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
}
