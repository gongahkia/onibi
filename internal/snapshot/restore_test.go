package snapshot

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRestoreSpawnsPTYWithReplaySeed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got, err := RestoreContext(ctx, Snapshot{
		SessionID:  "s1",
		Agent:      "shell",
		Command:    "sleep 5",
		CWD:        t.TempDir(),
		RingBuffer: []byte("vim screen"),
		Env:        []string{"PATH=" + os.Getenv("PATH")},
	}, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = got.Host.Close()
		_ = got.Host.Wait()
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if replay := got.Host.ReplaySince(0); bytes.Contains(replay.Data, []byte("vim screen")) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("replay = %q", got.Host.ReplaySince(0).Data)
}

func TestRestoreEnvFiltersSecretsAndAddsTranscriptMarker(t *testing.T) {
	env := restoreEnv(Snapshot{
		Transcript:       "/tmp/session.jsonl",
		TranscriptOffset: 42,
		Env: []string{
			"PATH=/bin",
			"SSH_AUTH_SOCK=/tmp/agent",
			"ONIBI_TOKEN_SECRET=x",
			"KEEP=1",
		},
	}, []string{"EXTRA=1"})
	if !slices.Contains(env, "PATH=/bin") || !slices.Contains(env, "KEEP=1") || !slices.Contains(env, "EXTRA=1") {
		t.Fatalf("env = %#v", env)
	}
	if !slices.Contains(env, "ONIBI_RESTORE_TRANSCRIPT=/tmp/session.jsonl") || !slices.Contains(env, "ONIBI_RESTORE_TRANSCRIPT_OFFSET=42") {
		t.Fatalf("env transcript markers = %#v", env)
	}
	for _, kv := range env {
		if strings.HasPrefix(kv, "SSH_") || strings.HasPrefix(kv, "ONIBI_TOKEN_") {
			t.Fatalf("secret env survived: %#v", env)
		}
	}
}

func TestRestoreRequiresCommandAndCWD(t *testing.T) {
	if _, err := Restore(Snapshot{CWD: t.TempDir()}); err == nil {
		t.Fatal("missing command did not fail")
	}
	if _, err := Restore(Snapshot{Command: "true"}); err == nil {
		t.Fatal("missing cwd did not fail")
	}
}
