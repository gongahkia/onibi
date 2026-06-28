package liveartifact

import (
	"os"
	"strings"
	"testing"
)

func TestRecorderWritesSanitizedArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDir, dir)
	t.Setenv("ONIBI_SECRET", "super-secret-token")
	rec, err := New("Test Provider", "ONIBI_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	rec.Record("event", map[string]any{"token": "super-secret-token", "msg": `PASSWORD="secret-value"`})
	if err := rec.Close("ONIBI_SECRET"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(rec.Path())
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, "super-secret-token") || strings.Contains(got, "secret-value") {
		t.Fatalf("artifact not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("artifact missing redaction marker: %s", got)
	}
}
