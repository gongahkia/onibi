package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/ghostty"
)

func TestWebDoctorRejectsImplicitCapture(t *testing.T) {
	cmd := webDoctorCmd()
	cmd.SetArgs([]string{"--read-target", "some-pane"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("implicit capture accepted")
	}
}

func TestLinuxWebDoctorDoesNotRequireGhostty(t *testing.T) {
	paths := config.Paths{StateDir: t.TempDir()}
	paths.Config = filepath.Join(paths.StateDir, "config.yaml")
	paths.Socket = filepath.Join(paths.StateDir, "absent.sock")
	cfg := config.Default()
	cfg.Multiplexer.Default = "tmux"
	// Passive inspection checks only executable presence; it must not run sh.
	cfg.Multiplexer.Tmux = config.MuxBackend{Bin: "/bin/sh", Config: "/dev/null"}
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	report := inspectWebHost(t.Context(), paths, "linux")
	cmd := webDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := writeWebDoctor(cmd, report, true); err != nil {
		t.Fatal(err, out.String())
	}
	for _, c := range report.Checks {
		if c.ID == "ghostty" && c.State != "not_applicable" {
			t.Fatal("Ghostty treated as a Linux prerequisite", c)
		}
	}
	cfg.Multiplexer.Tmux.Bin = filepath.Join(paths.StateDir, "missing-tmux")
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeWebDoctor(cmd, inspectWebHost(t.Context(), paths, "linux"), true); err == nil {
		t.Fatal("missing session backend reported ready")
	}
}

func TestWebDoctorJSONIsStructuredEvenWhenNotReady(t *testing.T) {
	cmd := webDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	report := ghostty.Diagnostics{Checks: []ghostty.Check{{ID: "automation", State: "unknown", Detail: "Not probed"}, {ID: "capture", State: "capture_permission", Detail: "Permission needed"}}}
	if err := writeWebDoctor(cmd, report, true); err == nil {
		t.Fatal("not-ready report succeeded")
	}
	var decoded ghostty.Diagnostics
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Checks) != 2 || decoded.Checks[0].State != "unknown" {
		t.Fatalf("%s", out.Bytes())
	}
}
