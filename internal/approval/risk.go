package approval

import (
	"encoding/json"
	"strings"
)

type Risk struct {
	Level   string
	Reasons []string
}

func ClassifyRisk(tool, inputJSON string) Risk {
	var m map[string]any
	_ = json.Unmarshal([]byte(inputJSON), &m)
	var reasons []string
	switch tool {
	case "Bash":
		cmd, _ := m["command"].(string)
		reasons = append(reasons, bashRisk(cmd)...)
	case "Write", "Edit", "MultiEdit":
		path, _ := m["file_path"].(string)
		reasons = append(reasons, pathRisk(path)...)
	}
	if len(reasons) == 0 {
		return Risk{Level: "low"}
	}
	level := "medium"
	for _, r := range reasons {
		if strings.Contains(r, "recursive delete") ||
			strings.Contains(r, "force push") ||
			strings.Contains(r, "disk write") ||
			strings.Contains(r, "secret-looking path") {
			level = "high"
			break
		}
	}
	return Risk{Level: level, Reasons: reasons}
}

func bashRisk(cmd string) []string {
	s := strings.ToLower(cmd)
	var out []string
	if strings.Contains(s, "rm -rf") || strings.Contains(s, "rm -fr") {
		out = append(out, "recursive delete")
	}
	if strings.Contains(s, "git push --force") || strings.Contains(s, "git push -f") {
		out = append(out, "force push")
	}
	if strings.Contains(s, "sudo ") {
		out = append(out, "privileged command")
	}
	if strings.Contains(s, "curl ") && strings.Contains(s, "|") && strings.Contains(s, "sh") {
		out = append(out, "remote script execution")
	}
	if strings.Contains(s, "wget ") && strings.Contains(s, "|") && strings.Contains(s, "sh") {
		out = append(out, "remote script execution")
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
	return dedupe(out)
}

func pathRisk(path string) []string {
	s := strings.ToLower(path)
	if secretLike(s) {
		return []string{"secret-looking path"}
	}
	return nil
}

func secretLike(s string) bool {
	for _, token := range []string{".env", "id_rsa", "id_ed25519", "password", "passwd", "secret", "token", "api_key", "apikey"} {
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
