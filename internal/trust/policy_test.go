package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParsePolicyAndEvaluateFirstMatch(t *testing.T) {
	p, err := Parse([]byte(`
[[rule]]
effect = "always_prompt"
expires = "10m"
[rule.match]
tool = "Edit"
path = "src/**/*.go"
agent = "claude"

[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Edit"
path = "src/**/*.go"
agent = "claude"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rules) != 2 {
		t.Fatalf("rules = %d", len(p.Rules))
	}
	if p.Rules[0].Expires != 10*time.Minute || p.Rules[0].Never {
		t.Fatalf("first expires = %#v", p.Rules[0])
	}
	if !p.Rules[1].Never {
		t.Fatalf("second rule never = false")
	}
	got, ok := p.Evaluate(Request{Tool: "Edit", Path: "src/internal/policy.go", Agent: "claude"})
	if !ok {
		t.Fatal("no match")
	}
	if got.Effect != EffectAlwaysPrompt {
		t.Fatalf("effect = %s", got.Effect)
	}
	if got.ID != "file:1" {
		t.Fatalf("id = %q", got.ID)
	}
}

func TestEvaluateRequiresAllMatchFields(t *testing.T) {
	p, err := Parse([]byte(`
[[rule]]
effect = "deny"
expires = "never"
[rule.match]
tool = "Bash"
path = "**/*.sh"
agent = "codex"
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Evaluate(Request{Tool: "Bash", Path: "scripts/release.sh", Agent: "claude"}); ok {
		t.Fatal("matched wrong agent")
	}
	got, ok := p.Evaluate(Request{Tool: "Bash", Path: "scripts/release.sh", Agent: "codex"})
	if !ok || got.Effect != EffectDeny {
		t.Fatalf("match = %#v ok=%v", got, ok)
	}
}

func TestRuntimeRuleExpiresAtWindow(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	p := Policy{Rules: []Rule{
		RuntimeRule(Match{Tool: "Edit", Path: "src/**", Agent: "claude"}, EffectAutoApprove, 5*time.Minute, now),
	}}
	req := Request{Tool: "Edit", Path: "src/main.go", Agent: "claude"}
	if got, ok := p.EvaluateAt(req, now.Add(5*time.Minute-time.Nanosecond)); !ok || got.Effect != EffectAutoApprove {
		t.Fatalf("match before expiry = %#v ok=%v", got, ok)
	}
	if _, ok := p.EvaluateAt(req, now.Add(5*time.Minute)); ok {
		t.Fatal("runtime rule matched at expiry")
	}
}

func TestExplainAtProducesStableMachineReadableTrace(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	p := Policy{Rules: []Rule{
		{Effect: EffectDeny, ExpiresRaw: "1m", Expires: time.Minute, ExpiresAt: now, Match: Match{Tool: "Bash"}},
		{Effect: EffectDeny, Never: true, ExpiresRaw: "never", Match: Match{Tool: "Edit", Agent: "codex"}},
		{Effect: EffectAutoApprove, Never: true, ExpiresRaw: "never", Match: Match{Tool: "Edit", Path: "src/**", Agent: "claude"}},
	}}
	req := Request{Tool: "Edit", Path: "src/main.go", Agent: "claude"}
	got := p.ExplainAt(req, now)
	again := p.ExplainAt(req, now)
	if !got.Matched || got.Effect != EffectAutoApprove || got.Rule == nil || got.Rule.ID != "file:3" {
		t.Fatalf("evaluation = %#v", got)
	}
	if outcomes := []string{got.Trace[0].Outcome, got.Trace[1].Outcome, got.Trace[2].Outcome}; !reflect.DeepEqual(outcomes, []string{"expired", "agent_mismatch", "matched"}) {
		t.Fatalf("trace = %#v", got.Trace)
	}
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("evaluation changed: %#v %#v", got, again)
	}
	b, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(b), `"trace"`) || strings.Contains(string(b), `"result"`) {
		t.Fatalf("json = %q err=%v", b, err)
	}
}

func TestLoadAnchorsFileExpiryAtModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.toml")
	if err := os.WriteFile(path, []byte(`[[rule]]
effect = "deny"
expires = "1m"
[rule.match]
tool = "Bash"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Now().UTC().Add(-2 * time.Minute)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rules) != 1 || !p.Rules[0].ExpiresAt.Equal(modified.Add(time.Minute)) {
		t.Fatalf("policy = %#v", p)
	}
	if got := p.ExplainAt(Request{Tool: "Bash"}, time.Now()); got.Matched || len(got.Trace) != 1 || got.Trace[0].Outcome != "expired" {
		t.Fatalf("evaluation = %#v", got)
	}
}

func TestParsePolicyRejectsInvalidRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "empty match", body: `effect = "deny"
expires = "never"`, want: "match required"},
		{name: "bad effect", body: `effect = "allow"
expires = "never"
[rule.match]
tool = "Edit"`, want: "invalid effect"},
		{name: "bad path", body: `effect = "deny"
expires = "never"
[rule.match]
path = "src/["`, want: "invalid path glob"},
		{name: "bad expires", body: `effect = "deny"
expires = "soon"
[rule.match]
tool = "Edit"`, want: "invalid expires"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte("[[rule]]\n" + tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
