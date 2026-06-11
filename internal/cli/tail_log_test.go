package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onibi.log")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := printTail(&out, path, 2); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "b\nc" {
		t.Fatalf("tail = %q", got)
	}
}
