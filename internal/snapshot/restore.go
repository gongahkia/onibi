package snapshot

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/pty"
)

type Session struct {
	ID               string
	Name             string
	Agent            string
	Command          string
	CWD              string
	Host             *pty.Host
	CreatedAt        time.Time
	Transcript       string
	TranscriptOffset int64
}

type RestoreOptions struct {
	EnvExtra []string
	Rows     uint16
	Cols     uint16
}

func Restore(s Snapshot) (Session, error) {
	return RestoreContext(context.Background(), s, RestoreOptions{})
}

func RestoreContext(ctx context.Context, s Snapshot, opts RestoreOptions) (Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command := strings.TrimSpace(s.Command)
	if command == "" {
		return Session{}, errors.New("snapshot: command required")
	}
	cwd := strings.TrimSpace(s.CWD)
	if cwd == "" {
		return Session{}, errors.New("snapshot: cwd required")
	}
	shell := restoreShell()
	host, err := pty.Spawn(ctx, pty.SpawnOptions{
		Name:       shell,
		Args:       []string{"-lc", command},
		Env:        restoreEnv(s, opts.EnvExtra),
		Dir:        cwd,
		Rows:       opts.Rows,
		Cols:       opts.Cols,
		ReplaceEnv: true,
	})
	if err != nil {
		return Session{}, err
	}
	host.SeedReplay(s.RingBuffer)
	return Session{
		ID:               strings.TrimSpace(s.SessionID),
		Name:             strings.TrimSpace(s.SessionName),
		Agent:            strings.TrimSpace(s.Agent),
		Command:          command,
		CWD:              cwd,
		Host:             host,
		CreatedAt:        time.Now().UTC(),
		Transcript:       strings.TrimSpace(s.Transcript),
		TranscriptOffset: s.TranscriptOffset,
	}, nil
}

func restoreShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func restoreEnv(s Snapshot, extra []string) []string {
	env := s.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	out := make([]string, 0, len(env)+len(extra)+2)
	for _, kv := range env {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" || strings.HasPrefix(key, "SSH_") || strings.HasPrefix(key, "ONIBI_TOKEN_") {
			continue
		}
		out = append(out, kv)
	}
	if strings.TrimSpace(s.Transcript) != "" {
		out = append(out, "ONIBI_RESTORE_TRANSCRIPT="+s.Transcript)
		out = append(out, "ONIBI_RESTORE_TRANSCRIPT_OFFSET="+strconv.FormatInt(s.TranscriptOffset, 10))
	}
	out = append(out, extra...)
	return out
}
