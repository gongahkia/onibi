package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/pty"
)

type fakeSession struct {
	id     string
	name   string
	agent  string
	cwd    string
	pid    int
	host   *pty.Host
	buffer []byte
}

func (s fakeSession) SnapshotID() string      { return s.id }
func (s fakeSession) SnapshotName() string    { return s.name }
func (s fakeSession) SnapshotAgent() string   { return s.agent }
func (s fakeSession) SnapshotCommand() string { return "echo ok" }
func (s fakeSession) SnapshotCWD() string     { return s.cwd }
func (s fakeSession) SnapshotPID() int        { return s.pid }
func (s fakeSession) SnapshotHost() *pty.Host { return s.host }
func (s fakeSession) SnapshotBuffer() []byte  { return append([]byte(nil), s.buffer...) }

func TestTakeCapturesRingAndTranscriptOffset(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	transcript := writeClaudeTranscript(t, base, root, "abc.jsonl", []byte(`{"usage":{"input_tokens":1,"output_tokens":2}}`+"\n"))
	got, err := TakeContext(context.Background(), fakeSession{
		id:     "s1",
		name:   "work",
		agent:  "claude",
		cwd:    root,
		buffer: []byte("screen marker"),
	}, Options{ClaudeBaseDir: base})
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "s1" || got.SessionName != "work" || got.Agent != "claude" {
		t.Fatalf("metadata = %#v", got)
	}
	if string(got.RingBuffer) != "screen marker" {
		t.Fatalf("ring = %q", got.RingBuffer)
	}
	if got.CWD != root {
		t.Fatalf("cwd = %q", got.CWD)
	}
	if got.Transcript != transcript || got.TranscriptOffset != int64(len(`{"usage":{"input_tokens":1,"output_tokens":2}}`+"\n")) {
		t.Fatalf("transcript=%q offset=%d", got.Transcript, got.TranscriptOffset)
	}
	if time.Since(got.CreatedAt) > time.Minute {
		t.Fatalf("created_at = %s", got.CreatedAt)
	}
}

func TestTakeCapturesLiveProcessCWDAndEnv(t *testing.T) {
	got, err := Take(fakeSession{
		id:    "s1",
		agent: "shell",
		cwd:   t.TempDir(),
		pid:   os.Getpid(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD == "" || !filepath.IsAbs(got.CWD) {
		t.Fatalf("cwd = %q", got.CWD)
	}
	if runtime.GOOS == "linux" && len(got.Env) == 0 {
		t.Fatalf("env = %#v", got.Env)
	}
}

func TestTakeFallsBackToSessionBufferWhenHostHasNoHub(t *testing.T) {
	got, err := Take(fakeSession{
		id:     "s1",
		agent:  "shell",
		cwd:    t.TempDir(),
		host:   pty.NewVirtualHost(nil, nil, nil),
		buffer: []byte("daemon screen"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.RingBuffer) != "daemon screen" {
		t.Fatalf("ring = %q", got.RingBuffer)
	}
}

func TestParseEnvHelpers(t *testing.T) {
	if got := parseNULenv([]byte("A=1\x00B=two\x00")); !slices.Equal(got, []string{"A=1", "B=two"}) {
		t.Fatalf("nul env = %#v", got)
	}
	if got := parsePSEnv([]byte("PID COMMAND\n1 /bin/zsh FOO=bar BAR=baz")); !slices.Equal(got, []string{"FOO=bar", "BAR=baz"}) {
		t.Fatalf("ps env = %#v", got)
	}
	cwd, err := parseLsofCWD([]byte("p1\nfcwd\nn/tmp/repo\n"))
	if err != nil || cwd != "/tmp/repo" {
		t.Fatalf("cwd=%q err=%v", cwd, err)
	}
}

func writeClaudeTranscript(t *testing.T, base, cwd, name string, body []byte) string {
	t.Helper()
	key, err := budget.ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "projects", key, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
