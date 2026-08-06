package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStageUploadIsPrivateBoundedAndExpires(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	b.d.UploadMaxBytes = 16
	b.d.UploadTTL = time.Hour
	s := NewSession("upload-1", "work", "shell", 4096)
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	path, expires, err := b.d.StageUpload(t.Context(), s.ID, "../../build file?.txt", 5, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(b.d.Paths.StateDir, "uploads", s.ID)
	if filepath.Dir(path) != root || strings.Contains(filepath.Base(path), " ") || !strings.HasSuffix(path, "build_file_.txt") {
		t.Fatalf("path=%q", path)
	}
	if expires.Before(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("expires=%s", expires)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("info=%#v err=%v", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "hello" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if _, _, err := b.d.StageUpload(t.Context(), s.ID, "large", 17, strings.NewReader("x")); err == nil {
		t.Fatal("accepted declared oversize upload")
	}
	if _, _, err := b.d.StageUpload(t.Context(), s.ID, "large", 0, strings.NewReader(strings.Repeat("x", 17))); err == nil {
		t.Fatal("accepted streamed oversize upload")
	}
	if err := os.Chtimes(path, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := b.d.PurgeExpiredUploads(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired upload remains: %v", err)
	}
}
