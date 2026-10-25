package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gongahkia/onibi/internal/ghostty"
)

func TestWebDoctorRejectsImplicitCapture(t *testing.T) {
	cmd := webDoctorCmd()
	cmd.SetArgs([]string{"--read-target", "some-pane"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("implicit capture accepted")
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
