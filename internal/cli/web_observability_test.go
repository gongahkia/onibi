package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/webadmin"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
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
	writeWebWelcome(cmd, link, "127.0.0.1:8787", "https://example.test", "/private/logs/web.log", "Running inside this server / persistent sessions survive restarts")
	if strings.Count(output.String(), link) != 1 || !strings.Contains(output.String(), "\n"+link+"\n") {
		t.Fatal("pair link is not a standalone line")
	}
	if events.Len() != 0 || strings.Contains(output.String(), "\x1b") {
		t.Fatal("redirected output has escapes or secret events")
	}
	native := "Not checked yet"
	if runtime.GOOS != "darwin" {
		native = "Persistent sessions / native Ghostty control is macOS-only"
	}
	for _, want := range []string{"Version", "Process", "Listening", native, "web status", "web logs --follow", "10 minutes", "Ctrl-C"} {
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

func TestStoppedStatusIsAResultWithoutSocketError(t *testing.T) {
	for _, cause := range []error{os.ErrNotExist, syscall.ECONNREFUSED} {
		for _, asJSON := range []bool{false, true} {
			cmd := webStatusCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			err := writeWebStatusResult(cmd, webadmin.Response{}, fmt.Errorf("dial private socket: %w", cause), asJSON)
			if err != nil {
				t.Fatal(err)
			}
			if asJSON {
				var status struct {
					Running bool
					State   string
				}
				if err := json.Unmarshal(out.Bytes(), &status); err != nil || status.Running || status.State != "stopped" {
					t.Fatalf("%s err=%v", out.String(), err)
				}
			} else {
				if !strings.Contains(out.String(), "Stopped") || !strings.Contains(out.String(), "second terminal") || strings.Contains(out.String(), "socket") {
					t.Fatal(out.String())
				}
			}
		}
	}
	cmd := webStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := writeWebStatusResult(cmd, webadmin.Response{}, os.ErrPermission, false); !errors.Is(err, os.ErrPermission) {
		t.Fatal("permission failure presented as stopped")
	}
	if strings.Contains(out.String(), "Stopped") {
		t.Fatal("false stopped state")
	}
}
func TestLogViewerCancellationAndJSONOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.log")
	record := `{"time":"2026-01-01T00:00:00Z","level":"INFO","msg":"web.started","pid":123}` + "\n"
	if err := os.WriteFile(path, []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
	for _, asJSON := range []bool{false, true} {
		cmd := webLogsCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.Flags().Set("follow", "true")
		cmd.Flags().Set("json", fmt.Sprint(asJSON))
		ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		defer cancel()
		cmd.SetContext(ctx)
		if !asJSON {
			terminalUI(cmd).Interactive = true
			terminalUI(cmd).ASCII = false
		}
		if err := runWebLogs(cmd, path); err != nil {
			t.Fatal(err)
		}
		before := out.String()
		time.Sleep(120 * time.Millisecond)
		if out.String() != before {
			t.Fatal("log viewer kept drawing after cancellation")
		}
		if asJSON {
			if out.String() != record {
				t.Fatalf("machine output decorated: %q", out.String())
			}
		} else {
			if !strings.Contains(before, "█") || !strings.Contains(before, "Following") || !strings.Contains(before, "Log follow ended.") {
				t.Fatal(before)
			}
			if strings.Contains(before, "web.started") {
				t.Fatal("replay uses a different event format")
			}
		}
	}
}
func TestMissingLogShowsStartGuidance(t *testing.T) {
	cmd := webLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := runWebLogs(cmd, filepath.Join(t.TempDir(), "missing.log"))
	if err == nil || !strings.Contains(err.Error(), "no web log yet") {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(out.String(), "Web cockpit / logs") {
		t.Fatal("missing command identity")
	}
}

func TestSourceRunSkipsOnlyTheSecondBanner(t *testing.T) {
	for _, shown := range []bool{false, true} {
		cmd := webCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.Flags().Set("no-banner", fmt.Sprint(shown))
		ui := terminalUI(cmd)
		ui.Interactive = true
		ui.ASCII = false
		writeWebWelcome(cmd, "http://127.0.0.1:8787/#token=test-only", "127.0.0.1:8787", "http://127.0.0.1:8787", "/tmp/web.log", "Running inside this server / persistent sessions survive restarts")
		if strings.Contains(out.String(), "█") == shown {
			t.Fatal("incorrect banner handoff")
		}
		for _, want := range []string{"PAIR THIS BROWSER", "#token=test-only", "ACTIVITY", "Ctrl-C stops Onibi"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("banner suppression hid %q", want)
			}
		}
	}
}
