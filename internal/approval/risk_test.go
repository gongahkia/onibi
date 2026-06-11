package approval

import "testing"

func TestClassifyRiskBashHigh(t *testing.T) {
	r := ClassifyRisk("Bash", `{"command":"rm -rf /tmp/x && git push --force"}`)
	if r.Level != "high" || len(r.Reasons) != 2 {
		t.Fatalf("risk = %+v", r)
	}
}

func TestClassifyRiskSecretPath(t *testing.T) {
	r := ClassifyRisk("Write", `{"file_path":".env","content":"x"}`)
	if r.Level != "high" || r.Reasons[0] != "secret-looking path" {
		t.Fatalf("risk = %+v", r)
	}
}

func TestClassifyRiskLow(t *testing.T) {
	r := ClassifyRisk("Bash", `{"command":"go test ./..."}`)
	if r.Level != "low" || len(r.Reasons) != 0 {
		t.Fatalf("risk = %+v", r)
	}
}
