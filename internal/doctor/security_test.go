package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityScanFindsAllPatternsAndRedacts(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	samples := map[string]string{
		"github_pat":         "ghp_" + strings.Repeat("A", 36),
		"github_server":      "ghs_" + strings.Repeat("B", 36),
		"github_oauth":       "gho_" + strings.Repeat("C", 36),
		"openai":             "sk-" + strings.Repeat("D", 20),
		"anthropic":          "sk-ant-" + strings.Repeat("E", 30),
		"slack":              "xoxb-" + strings.Repeat("F", 10),
		"aws_access_key":     "AKIA" + strings.Repeat("G", 16),
		"aws_session_key":    "ASIA" + strings.Repeat("H", 16),
		"google_api_key":     "AIza" + strings.Repeat("I", 35),
		"huggingface":        "hf_" + strings.Repeat("J", 34),
		"bearer":             "Bearer " + strings.Repeat("K", 21),
		"private_key_header": "-----BEGIN OPENSSH PRIVATE KEY-----",
	}
	var log strings.Builder
	for name, sample := range samples {
		log.WriteString(name + " leaked " + sample + " suffix\n")
	}
	if err := os.WriteFile(filepath.Join(paths.LogDir, "onibi.log"), []byte(log.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Security(t.Context(), Options{Paths: paths})
	if report.Status != Fail {
		t.Fatalf("status = %s, want %s", report.Status, Fail)
	}
	got := map[string]bool{}
	for _, finding := range report.Findings {
		got[finding.Pattern] = true
		for name, sample := range samples {
			if strings.Contains(finding.Snippet, sample) {
				t.Fatalf("%s token leaked in snippet for %s: %q", name, finding.Pattern, finding.Snippet)
			}
		}
	}
	for name := range samples {
		if !got[name] {
			t.Fatalf("missing pattern %s in %#v", name, report.Findings)
		}
	}
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for name, sample := range samples {
		if strings.Contains(string(body), sample) {
			t.Fatalf("%s token leaked in JSON: %s", name, body)
		}
	}
}

func TestSecurityScanPassesWithNoLogFiles(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	if err := os.WriteFile(filepath.Join(paths.LogDir, "skip.txt"), []byte("ghp_"+strings.Repeat("A", 36)), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Security(t.Context(), Options{Paths: paths})
	if report.Status != Pass || len(report.Findings) != 0 || report.ScannedFiles != 0 {
		t.Fatalf("report = %#v", report)
	}
}
