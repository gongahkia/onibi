package budget

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePolicy(t *testing.T) {
	p, err := ParsePolicy([]byte(`
[global]
max_tokens_per_day = 100000

[session]
max_tokens = 25000
on_overrun = "kill"
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Global.MaxTokensPerDay != 100000 || p.Session.MaxTokens != 25000 || p.Session.OnOverrun != OverrunKill {
		t.Fatalf("policy = %#v", p)
	}
}

func TestParsePolicyDefaults(t *testing.T) {
	p, err := ParsePolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Session.OnOverrun != OverrunInterrupt || p.Global.MaxTokensPerDay != 0 || p.Session.MaxTokens != 0 {
		t.Fatalf("defaults = %#v", p)
	}
	p, err = ParsePolicy([]byte("[session]\nmax_tokens = 10\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Session.OnOverrun != OverrunInterrupt {
		t.Fatalf("on_overrun = %q", p.Session.OnOverrun)
	}
}

func TestParsePolicyRejectsInvalidValues(t *testing.T) {
	tests := []string{
		"[global]\nmax_tokens_per_day = -1\n",
		"[session]\nmax_tokens = -1\n",
		"[session]\non_overrun = \"pause\"\n",
	}
	for _, input := range tests {
		if _, err := ParsePolicy([]byte(input)); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestLoadPolicyDefaultsWhenMissing(t *testing.T) {
	p, err := LoadPolicy(filepath.Join(t.TempDir(), ".onibi", "budget.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Session.OnOverrun != OverrunInterrupt {
		t.Fatalf("policy = %#v", p)
	}
}

func TestLoadPolicyReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".onibi", "budget.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[session]\non_overrun = \"warn\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Session.OnOverrun != OverrunWarn {
		t.Fatalf("policy = %#v", p)
	}
}

func TestPolicyPath(t *testing.T) {
	got := PolicyPath("/repo")
	if !strings.HasSuffix(got, "/repo/.onibi/budget.toml") {
		t.Fatalf("path = %q", got)
	}
}

func TestEstimateCostMatchesPublishedRates(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		input      int64
		output     int64
		microCents int64
		usd        float64
	}{
		{name: "sonnet", model: "claude-sonnet-4-6", input: 1_000_000, output: 1_000_000, microCents: 1800 * MicroCentsPerCent, usd: 18},
		{name: "opus", model: "claude-opus-4-7", input: 2_000_000, output: 3_000_000, microCents: 8500 * MicroCentsPerCent, usd: 85},
		{name: "haiku", model: "claude-haiku-4-5", input: 1, output: 1, microCents: 600, usd: 0.000006},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := EstimateCost(tt.model, tt.input, tt.output)
			if !ok {
				t.Fatal("rate missing")
			}
			if got.TotalMicroCents != tt.microCents {
				t.Fatalf("microcents = %d, want %d", got.TotalMicroCents, tt.microCents)
			}
			if math.Abs(got.USD()-tt.usd) > 0.000000001 {
				t.Fatalf("usd = %v, want %v", got.USD(), tt.usd)
			}
		})
	}
}

func TestEstimateCostRejectsUnknownOrNegative(t *testing.T) {
	if _, ok := EstimateCost("claude-unknown", 1, 1); ok {
		t.Fatal("unknown model matched")
	}
	if _, ok := EstimateCost("claude-sonnet-4-6", -1, 1); ok {
		t.Fatal("negative input matched")
	}
	if _, ok := EstimateCost("claude-sonnet-4-6", maxInt64, 1); ok {
		t.Fatal("overflow matched")
	}
}
