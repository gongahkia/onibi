package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/pty"
)

type Source interface {
	SnapshotID() string
	SnapshotName() string
	SnapshotAgent() string
	SnapshotCommand() string
	SnapshotCWD() string
	SnapshotPID() int
	SnapshotHost() *pty.Host
	SnapshotBuffer() []byte
}

type Snapshot struct {
	SessionID        string
	SessionName      string
	Agent            string
	Command          string
	PID              int
	CreatedAt        time.Time
	RingBuffer       []byte
	CWD              string
	Env              []string
	Transcript       string
	TranscriptOffset int64
}

type Options struct {
	ClaudeBaseDir string
}

var execCommandContext = exec.CommandContext

func Take(s Source) (Snapshot, error) {
	return TakeContext(context.Background(), s, Options{})
}

func TakeContext(ctx context.Context, s Source, opts Options) (Snapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return Snapshot{}, errors.New("snapshot: session required")
	}
	id := strings.TrimSpace(s.SnapshotID())
	if id == "" {
		return Snapshot{}, errors.New("snapshot: session id required")
	}
	pid := s.SnapshotPID()
	cwd, err := captureCWD(ctx, pid, s.SnapshotCWD())
	if err != nil {
		return Snapshot{}, err
	}
	env, err := captureEnv(ctx, pid)
	if err != nil {
		return Snapshot{}, err
	}
	out := Snapshot{
		SessionID:   id,
		SessionName: strings.TrimSpace(s.SnapshotName()),
		Agent:       strings.TrimSpace(s.SnapshotAgent()),
		Command:     strings.TrimSpace(s.SnapshotCommand()),
		PID:         pid,
		CreatedAt:   time.Now().UTC(),
		RingBuffer:  captureRing(s),
		CWD:         cwd,
		Env:         env,
	}
	if strings.EqualFold(out.Agent, "claude") {
		out.Transcript, out.TranscriptOffset = currentClaudeTranscript(opts.ClaudeBaseDir, out.SessionID, cwd)
	}
	return out, nil
}

func captureRing(s Source) []byte {
	if h := s.SnapshotHost(); h != nil {
		if replay := h.ReplaySince(0); len(replay.Data) > 0 {
			return append([]byte(nil), replay.Data...)
		}
	}
	return append([]byte(nil), s.SnapshotBuffer()...)
}

func captureCWD(ctx context.Context, pid int, fallback string) (string, error) {
	fallback = strings.TrimSpace(fallback)
	if pid > 0 {
		cwd, err := processCWD(ctx, pid)
		if err == nil && strings.TrimSpace(cwd) != "" {
			return filepath.Clean(cwd), nil
		}
		if err == nil {
			err = errors.New("empty cwd")
		}
		if fallback == "" {
			return "", fmt.Errorf("snapshot: cwd for pid %d: %w", pid, err)
		}
	}
	if fallback == "" {
		return "", errors.New("snapshot: cwd required")
	}
	return filepath.Clean(fallback), nil
}

func captureEnv(ctx context.Context, pid int) ([]string, error) {
	if pid <= 0 {
		return nil, nil
	}
	env, err := processEnv(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("snapshot: env for pid %d: %w", pid, err)
	}
	return env, nil
}

func processCWD(ctx context.Context, pid int) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	case "darwin":
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := execCommandContext(cctx, "lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
		if err != nil {
			return "", err
		}
		return parseLsofCWD(out)
	default:
		return "", fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

func processEnv(ctx context.Context, pid int) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
		if err != nil {
			return nil, err
		}
		return parseNULenv(data), nil
	case "darwin":
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := execCommandContext(cctx, "ps", "eww", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return nil, err
		}
		return parsePSEnv(out), nil
	default:
		return nil, fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

func parseLsofCWD(out []byte) (string, error) {
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		if len(line) > 1 && line[0] == 'n' {
			return string(line[1:]), nil
		}
	}
	return "", errors.New("cwd not found")
}

func parseNULenv(data []byte) []string {
	var env []string
	for _, part := range bytes.Split(data, []byte{0}) {
		if len(part) > 0 {
			env = append(env, string(part))
		}
	}
	return env
}

var envAssignRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

func parsePSEnv(out []byte) []string {
	var env []string
	for _, field := range strings.Fields(string(out)) {
		if envAssignRE.MatchString(field) {
			env = append(env, field)
		}
	}
	return env
}

func currentClaudeTranscript(baseDir, sessionID, cwd string) (string, int64) {
	path, err := budget.NewClaudeParser(baseDir).FindTranscript(budget.SessionRef{
		SessionID: sessionID,
		Agent:     "claude",
		CWD:       cwd,
	})
	if err != nil {
		return "", 0
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", 0
	}
	return path, info.Size()
}
