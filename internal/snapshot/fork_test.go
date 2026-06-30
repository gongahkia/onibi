package snapshot

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForkTruncatesTranscriptInjectsPromptAndRestores(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	body := strings.Join([]string{
		`{"type":"user","message":{"content":"one"}}`,
		`{"type":"assistant","message":{"content":"two"}}`,
		`{"type":"assistant","message":{"content":"drop"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got, err := ForkContext(ctx, Snapshot{
		SessionID:        "s1",
		Agent:            "claude",
		Command:          "sleep 5",
		CWD:              dir,
		RingBuffer:       []byte("mid-screen"),
		Transcript:       transcript,
		TranscriptOffset: int64(len(body)),
		Env:              []string{"PATH=" + os.Getenv("PATH")},
	}, 2, "branch prompt", RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = got.Host.Close()
		_ = got.Host.Wait()
	}()
	data, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "drop") || !strings.Contains(text, "branch prompt") {
		t.Fatalf("transcript = %s", text)
	}
	if lines := strings.Count(strings.TrimSpace(text), "\n") + 1; lines != 3 {
		t.Fatalf("line count = %d transcript=%s", lines, text)
	}
	if got.TranscriptOffset != int64(len(data)) {
		t.Fatalf("offset = %d want %d", got.TranscriptOffset, len(data))
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if replay := got.Host.ReplaySince(0); bytes.Contains(replay.Data, []byte("mid-screen")) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("replay = %q", got.Host.ReplaySince(0).Data)
}

func TestForkValidation(t *testing.T) {
	if _, err := Fork(Snapshot{Transcript: "x"}, -1, "prompt"); err == nil {
		t.Fatal("negative atTurn did not fail")
	}
	if _, err := Fork(Snapshot{Transcript: "x"}, 0, " "); err == nil {
		t.Fatal("empty prompt did not fail")
	}
	if _, err := Fork(Snapshot{}, 0, "prompt"); err == nil {
		t.Fatal("missing transcript did not fail")
	}
}
