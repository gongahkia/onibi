package anomaly

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvaluateRuleFixtures(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		fixture string
		opts    Options
		want    string
	}{
		{fixture: "write-burst", opts: Options{WorkspaceRoot: root}, want: RuleWriteBurst},
		{fixture: "fork-bomb", want: RuleForkBomb},
		{fixture: "exfil-host", opts: Options{NetworkAllowlist: []string{"github.com"}}, want: RuleExfilHost},
		{fixture: "secret-args", want: RuleSecretArgs},
		{fixture: "reverse-shell", want: RuleReverseShell},
		{fixture: "curl-pipe-shell", opts: Options{NetworkAllowlist: []string{"install.example"}}, want: RuleCurlPipeShell},
		{fixture: "outside-workspace-write", opts: Options{WorkspaceRoot: root}, want: RuleOutsideWorkspace},
		{fixture: "tool-loop", want: RuleToolLoop},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			findings := Evaluate(loadTranscript(t, tc.fixture, root), tc.opts)
			if len(findings) != 1 || findings[0].RuleName != tc.want {
				t.Fatalf("findings = %#v, want exactly %s", findings, tc.want)
			}
		})
	}
}

func TestEvaluateCleanControlFixture(t *testing.T) {
	root := t.TempDir()
	actions := loadTranscript(t, "clean-control", root)
	findings := Evaluate(actions, Options{WorkspaceRoot: root, NetworkAllowlist: []string{"github.com"}})
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
}

func TestLoadOptionsReadsNetworkAllowlist(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".onibi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".onibi", "network.toml"), []byte("[network]\nallowlist = [\"example.com\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := LoadOptions(root)
	if err != nil {
		t.Fatal(err)
	}
	findings := Evaluate([]Action{bash(`{"command":"curl https://api.example.com"}`, time.Now())}, opts)
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want allowlisted host", findings)
	}
}

func loadTranscript(t *testing.T, fixture, root string) []Action {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", fixture+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var actions []Action
	scanner := bufio.NewScanner(f)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var action Action
		if err := json.Unmarshal([]byte(line), &action); err != nil {
			t.Fatalf("%s:%d: %v", fixture, lineNo, err)
		}
		action.CWD = strings.ReplaceAll(action.CWD, "$ROOT", root)
		action.FilePath = strings.ReplaceAll(action.FilePath, "$ROOT", root)
		action.InputJSON = strings.ReplaceAll(action.InputJSON, "$ROOT", root)
		actions = append(actions, action)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actions) == 0 {
		t.Fatalf("%s: empty transcript", fixture)
	}
	return actions
}

func bash(input string, at time.Time) Action {
	return Action{SessionID: "s1", Tool: "Bash", InputJSON: input, At: at}
}
