package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/intake"
)

func TestApprovalUnifiedDiffWriteScrubsBeforeDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	oldText := "token = \"oldsecret\"\nmode = \"old\"\n"
	newText := "token = \"newsecret\"\nmode = \"new\"\n"
	if err := os.WriteFile(path, []byte(oldText), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]any{
		"file_path": "config.env",
		"content":   newText,
	})
	if err != nil {
		t.Fatal(err)
	}
	diff := approvalUnifiedDiff(intake.Event{
		Tool:      "Write",
		CWD:       dir,
		InputJSON: string(input),
	})
	if diff == "" {
		t.Fatal("empty diff")
	}
	if strings.Contains(diff, "oldsecret") || strings.Contains(diff, "newsecret") {
		t.Fatalf("diff leaked secret: %s", diff)
	}
	if !strings.Contains(diff, "[REDACTED]") || !strings.Contains(diff, "+mode = \"new\"") {
		t.Fatalf("diff = %s", diff)
	}
}
