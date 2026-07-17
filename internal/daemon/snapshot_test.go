package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/transcript"
)

func TestSnapshotRPCTakeListDelete(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := New(Options{DB: db})
	root := t.TempDir()
	s := NewSession("s1", "claude", "claude", pty.NewVirtualHost(nil, nil, nil), 128)
	s.CWD = root
	s.Cmd = "echo ok"
	_, _ = s.Buf.Write([]byte("screen"))
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.persistAndCheckSession(t, s); err != nil {
		t.Fatal(err)
	}
	resp, err := d.handleSnapshotRPC(t.Context(), intake.Event{Type: intake.TypeSnapshot, SnapshotAction: "take", Session: "s1", SnapshotName: "snap1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Text, "saved") {
		t.Fatalf("take = %#v", resp)
	}
	resp, err = d.handleSnapshotRPC(t.Context(), intake.Event{Type: intake.TypeSnapshot, SnapshotAction: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Text, "snap1") || !strings.Contains(resp.Text, root) {
		t.Fatalf("list = %#v", resp)
	}
	resp, err = d.handleSnapshotRPC(t.Context(), intake.Event{Type: intake.TypeSnapshot, SnapshotAction: "delete", SnapshotName: "snap1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Text, "deleted") {
		t.Fatalf("delete = %#v", resp)
	}
}

func TestSnapshotRPCRestoreAndForkRoundTrip(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	root := t.TempDir()
	claudeBase := t.TempDir()
	transcript := writeDaemonClaudeTranscript(t, claudeBase, root)
	d := New(Options{DB: db})
	d.claudeBaseDir = claudeBase
	src := NewSession("s1", "claude", "claude", pty.NewVirtualHost(nil, nil, nil), 128)
	src.CWD = root
	src.Cmd = "sleep 5"
	if err := d.persistAndCheckSession(t, src); err != nil {
		t.Fatal(err)
	}
	if err := db.SnapshotSave(t.Context(), store.SnapshotEntry{
		ID:               "snap1",
		SessionID:        "s1",
		Name:             "branch",
		CreatedAt:        time.Now().UTC(),
		RingBuffer:       []byte("vim-state"),
		CWD:              root,
		Env:              []string{"PATH=" + os.Getenv("PATH")},
		TranscriptOffset: 1,
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := d.handleSnapshotRPC(t.Context(), intake.Event{Type: intake.TypeSnapshot, SnapshotAction: "restore", SnapshotName: "branch"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SessionID == "" || !strings.Contains(resp.Text, "restored branch") {
		t.Fatalf("restore = %#v", resp)
	}
	closeDaemonSession(t, d, resp.SessionID)
	resp, err = d.handleSnapshotRPC(t.Context(), intake.Event{
		Type:           intake.TypeSnapshot,
		SnapshotAction: "fork",
		SnapshotName:   "branch",
		SnapshotTurn:   1,
		Text:           "new branch prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SessionID == "" || !strings.Contains(resp.Text, "forked branch") {
		t.Fatalf("fork = %#v", resp)
	}
	closeDaemonSession(t, d, resp.SessionID)
	body, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "drop me") || !strings.Contains(string(body), "new branch prompt") {
		t.Fatalf("fork transcript = %s", string(body))
	}
}

func (d *Daemon) persistAndCheckSession(t *testing.T, s *Session) error {
	t.Helper()
	d.persistSessionStart(t.Context(), s, s.CWD)
	_, ok, err := d.DB.SessionByID(t.Context(), s.ID)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		return errors.New("session not persisted")
	}
	return nil
}

func writeDaemonClaudeTranscript(t *testing.T, base, cwd string) string {
	t.Helper()
	key, err := transcript.ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "projects", key, "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","message":{"content":"keep me"}}`,
		`{"type":"assistant","message":{"content":"drop me"}}`,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func closeDaemonSession(t *testing.T, d *Daemon, id string) {
	t.Helper()
	s, err := d.Registry.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if s.Host != nil {
		_ = s.Host.Close()
	}
}
