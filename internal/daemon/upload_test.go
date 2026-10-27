package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStageUploadIsPrivateBoundedAndExpires(t *testing.T) {
	d, _ := testDaemon(t)
	d.UploadMaxBytes = 16
	d.UploadTTL = time.Hour
	s := NewSession("upload-1", "work", "shell", 4096)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	path, expires, err := d.StageUpload(t.Context(), s.ID, "../../build file?.txt", 5, strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(d.Paths.StateDir, "uploads", s.ID)
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
	if _, _, err := d.StageUpload(t.Context(), s.ID, "large", 17, strings.NewReader("x")); err == nil {
		t.Fatal("accepted declared oversize upload")
	}
	if _, _, err := d.StageUpload(t.Context(), s.ID, "large", 0, strings.NewReader(strings.Repeat("x", 17))); err == nil {
		t.Fatal("accepted streamed oversize upload")
	}
	if err := os.Chtimes(path, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := d.PurgeExpiredUploads(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired upload remains: %v", err)
	}
}

func TestStageUploadRejectsIncompleteOrMismatchedDownloads(t *testing.T) {
	d, _ := testDaemon(t)
	s := NewSession("upload-integrity", "work", "shell", 4096)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"short", "too much data"} {
		if _, _, err := d.StageUpload(t.Context(), s.ID, "document.txt", 10, strings.NewReader(body)); err == nil || !strings.Contains(err.Error(), "size mismatch") {
			t.Fatalf("body=%q err=%v", body, err)
		}
		files, err := os.ReadDir(filepath.Join(d.Paths.StateDir, "uploads", s.ID))
		if err != nil || len(files) != 0 {
			t.Fatalf("mismatched upload left staged bytes: files=%v err=%v", files, err)
		}
	}
	// An unknown size remains bounded by
	// UploadMaxBytes and is accepted after a complete read.
	if _, _, err := d.StageUpload(t.Context(), s.ID, "unknown.txt", 0, strings.NewReader("complete")); err != nil {
		t.Fatal(err)
	}
}
