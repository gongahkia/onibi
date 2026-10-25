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

func TestWelcomeKeepsPairingLinkCopyableAndOutOfEventStream(t *testing.T) {
	var output, events bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	cmd.SetErr(&events)
	link := "https://example.test/#token=test-only-secret"
	writeWebWelcome(cmd, link, "127.0.0.1:8787", "https://example.test", "/private/logs/web.log")
	if strings.Count(output.String(), link) != 1 || !strings.Contains(output.String(), "\n"+link+"\n") {
		t.Fatal("pair link is not a standalone line")
	}
	if events.Len() != 0 || strings.Contains(output.String(), "\x1b") {
		t.Fatal("redirected output has escapes or secret events")
	}
	for _, want := range []string{"Version", "Process", "Listening", "Not checked yet", "web status", "web logs --follow", "10 minutes", "Ctrl-C"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
}
func TestVersionJSONRemainsMachineReadable(t *testing.T) {
	var output bytes.Buffer
	cmd := Root()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--plain", "version", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var data map[string]string
	if err := json.Unmarshal(output.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["go"] == "" || data["platform"] == "" {
		t.Fatal("missing provenance")
	}
}
