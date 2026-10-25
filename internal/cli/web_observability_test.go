package cli

import (
	"bytes"
	"encoding/json"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/spf13/cobra"
	"strings"
	"testing"
)

func TestRuntimeStatusPresentationAndJSON(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&out)
		status := web.RuntimeStatus{PID: 42, Version: "test", Commit: "abc", Inventory: web.Observation{State: "unknown"}, Capture: web.Observation{State: "ready"}}
		if err := writeWebStatus(cmd, status, asJSON); err != nil {
			t.Fatal(err)
		}
		if asJSON {
			if !json.Valid(out.Bytes()) {
				t.Fatal(out.String())
			}
		} else if !strings.Contains(out.String(), "unknown") || strings.Contains(out.String(), "\x1b") {
			t.Fatal(out.String())
		}
	}
}
func TestWebCommandsExposeInspectionWithoutStarting(t *testing.T) {
	for _, args := range [][]string{{"web", "logs", "--help"}, {"web", "status", "--help"}, {"web", "--help"}} {
		cmd := Root()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			t.Fatal("empty help")
		}
	}
}
