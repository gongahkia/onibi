package approval

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/anomaly"
)

type RiskLevel = string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type Risk struct {
	Level   RiskLevel `json:"level"`
	Reasons []string  `json:"reasons,omitempty"`
}

type RiskEvent struct {
	SessionID      string
	Agent          string
	Tool           string
	InputJSON      string
	Command        string
	FilePath       string
	CWD            string
	At             time.Time
	Turn           int
	History        []anomaly.Action
	AnomalyOptions anomaly.Options
}

func ClassifyRisk(tool, inputJSON string) Risk {
	return ClassifyEventRisk(RiskEvent{Tool: tool, InputJSON: inputJSON})
}

func ClassifyEventRisk(ev RiskEvent) Risk {
	return combineRisk(staticRisk(ev.Tool, ev.InputJSON), behaviorRisk(ev))
}

func staticRisk(tool, inputJSON string) Risk {
	var m map[string]any
	_ = json.Unmarshal([]byte(inputJSON), &m)
	var reasons []string
	details := ExtractDetails(tool, inputJSON)
	switch tool {
	case "Bash":
		reasons = append(reasons, bashRisk(details.Command)...)
	case "Write", "Edit", "MultiEdit":
		reasons = append(reasons, pathRisk(details.FilePath)...)
	default:
		reasons = append(reasons, bashRisk(details.Command)...)
		reasons = append(reasons, pathRisk(details.FilePath)...)
	}
	if len(reasons) == 0 {
		return Risk{Level: RiskLow}
	}
	level := RiskMedium
	for _, r := range reasons {
		if strings.Contains(r, "recursive delete") ||
			strings.Contains(r, "destructive shell") ||
			strings.Contains(r, "force push") ||
			strings.Contains(r, "git rewrite") ||
			strings.Contains(r, "disk write") ||
			strings.Contains(r, "secret-looking path") ||
			strings.Contains(r, "credential file") ||
			strings.Contains(r, "package publish") ||
			strings.Contains(r, "production-looking target") {
			level = RiskHigh
			break
		}
	}
	return Risk{Level: level, Reasons: reasons}
}

func behaviorRisk(ev RiskEvent) Risk {
	action := anomaly.Action{
		SessionID: ev.SessionID,
		Agent:     ev.Agent,
		Tool:      ev.Tool,
		InputJSON: ev.InputJSON,
		Command:   ev.Command,
		FilePath:  ev.FilePath,
		CWD:       ev.CWD,
		At:        ev.At,
		Turn:      ev.Turn,
	}
	findings := anomaly.EvaluateOne(ev.History, action, ev.AnomalyOptions)
	var reasons []string
	for _, finding := range findings {
		if finding.RuleName == anomaly.RuleExfilHost && !hasNetworkPolicy(ev.AnomalyOptions) {
			continue
		}
		reasons = append(reasons, "anomaly: "+finding.RuleName)
	}
	if len(reasons) == 0 {
		return Risk{Level: RiskLow}
	}
	return Risk{Level: RiskHigh, Reasons: dedupe(reasons)}
}

func hasNetworkPolicy(opts anomaly.Options) bool {
	return strings.TrimSpace(opts.WorkspaceRoot) != "" || len(opts.NetworkAllowlist) > 0
}

func combineRisk(static, behavior Risk) Risk {
	level := maxRiskLevel(static.Level, behavior.Level)
	reasons := append(append([]string(nil), static.Reasons...), behavior.Reasons...)
	return Risk{Level: level, Reasons: dedupe(reasons)}
}

func maxRiskLevel(a, b RiskLevel) RiskLevel {
	if riskRank(b) > riskRank(a) {
		return b
	}
	if a == "" {
		return RiskLow
	}
	return a
}

func riskRank(level RiskLevel) int {
	switch level {
	case RiskHigh:
		return 3
	case RiskMedium:
		return 2
	default:
		return 1
	}
}

func bashRisk(cmd string) []string {
	s := strings.ToLower(cmd)
	var out []string
	if strings.Contains(s, "rm -rf") || strings.Contains(s, "rm -fr") {
		out = append(out, "recursive delete")
	} else if strings.Contains(s, " rm ") || strings.HasPrefix(s, "rm ") {
		out = append(out, "destructive shell")
	}
	if strings.Contains(s, "git push --force") || strings.Contains(s, "git push -f") {
		out = append(out, "force push")
	}
	for _, token := range []string{"git reset --hard", "git rebase", "git filter-branch"} {
		if strings.Contains(s, token) {
			out = append(out, "git rewrite")
			break
		}
	}
	if strings.Contains(s, "sudo ") {
		out = append(out, "privileged command")
	}
	for _, token := range []string{"chmod ", "chown "} {
		if strings.Contains(s, token) || strings.HasPrefix(s, token) {
			out = append(out, "permission change")
			break
		}
	}
	if strings.Contains(s, "curl ") && strings.Contains(s, "|") && strings.Contains(s, "sh") {
		out = append(out, "remote script execution")
	}
	if strings.Contains(s, "wget ") && strings.Contains(s, "|") && strings.Contains(s, "sh") {
		out = append(out, "remote script execution")
	}
	for _, token := range []string{"curl ", "wget ", "scp ", "ssh ", "nc ", "netcat "} {
		if strings.Contains(s, token) || strings.HasPrefix(s, token) {
			out = append(out, "network")
			break
		}
	}
	for _, token := range []string{"npm publish", "pnpm publish", "yarn npm publish", "twine upload", "cargo publish", "gem push"} {
		if strings.Contains(s, token) {
			out = append(out, "package publish")
			break
		}
	}
	for _, token := range []string{"mkfs", "diskutil erase", "dd if=", "dd of="} {
		if strings.Contains(s, token) {
			out = append(out, "disk write")
			break
		}
	}
	if secretLike(s) {
		out = append(out, "secret-looking args")
	}
	if productionLike(s) {
		out = append(out, "production-looking target")
	}
	return dedupe(out)
}

func pathRisk(path string) []string {
	s := strings.ToLower(path)
	var out []string
	if secretLike(s) {
		out = append(out, "secret-looking path", "credential file")
	}
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "/tmp/") && !strings.HasPrefix(s, "/var/folders/") {
		out = append(out, "absolute file target")
	}
	if productionLike(s) {
		out = append(out, "production-looking target")
	}
	return dedupe(out)
}

func secretLike(s string) bool {
	for _, token := range []string{".env", "id_rsa", "id_ed25519", "password", "passwd", "secret", "token", "api_key", "apikey"} {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

func productionLike(s string) bool {
	for _, token := range []string{"prod", "production", "staging", "live"} {
		if strings.Contains(s, token) {
			return true
		}
	}
	return false
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
