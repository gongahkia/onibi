package transcript

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ClaudeProjectKey(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", errors.New("cwd required")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(filepath.ToSlash(filepath.Clean(abs)), "/", "-"), nil
}

func FindClaude(baseDir, providerSessionID, cwd string) (string, error) {
	base, err := claudeBaseDir(baseDir)
	if err != nil {
		return "", err
	}
	projectKey, err := ClaudeProjectKey(cwd)
	if err != nil {
		return "", err
	}
	projectDir := filepath.Join(base, "projects", projectKey)
	providerSessionID = strings.TrimSpace(providerSessionID)
	if providerSessionID == "" {
		return newestClaudeTranscript(projectDir)
	}
	for _, path := range []string{
		filepath.Join(projectDir, "sessions", providerSessionID+".jsonl"),
		filepath.Join(projectDir, providerSessionID+".jsonl"),
		filepath.Join(projectDir, "sessions", providerSessionID, "session.jsonl"),
		filepath.Join(projectDir, providerSessionID, providerSessionID+".jsonl"),
	} {
		if isFile(path) {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func claudeBaseDir(baseDir string) (string, error) {
	if strings.TrimSpace(baseDir) != "" {
		return filepath.Abs(baseDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

func newestClaudeTranscript(projectDir string) (string, error) {
	var best string
	var bestMod time.Time
	for _, pattern := range []string{filepath.Join(projectDir, "*.jsonl"), filepath.Join(projectDir, "sessions", "*.jsonl")} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		for _, path := range matches {
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			if best == "" || info.ModTime().After(bestMod) {
				best = path
				bestMod = info.ModTime()
			}
		}
	}
	if best == "" {
		return "", os.ErrNotExist
	}
	return best, nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
