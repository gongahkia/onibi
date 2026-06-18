package approval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Details struct {
	Target   string
	Command  string
	FilePath string
}

func ExtractDetails(tool, inputJSON string) Details {
	var m map[string]any
	if err := json.Unmarshal([]byte(inputJSON), &m); err != nil {
		return Details{}
	}
	d := Details{
		Command:  firstString(m, "command", "cmd", "shellCommand", "script", "bash"),
		FilePath: firstString(m, "file_path", "filePath", "filepath", "path", "uri", "file", "target", "targetPath"),
	}
	lowTool := strings.ToLower(tool)
	if strings.Contains(lowTool, "grep") || strings.Contains(lowTool, "glob") {
		if pattern := firstString(m, "pattern", "query", "glob", "regex"); pattern != "" {
			d.Target = pattern
			return d
		}
	}
	targets := []string{
		d.Command,
		d.FilePath,
		firstString(m, "url", "webUrl", "href"),
		firstString(m, "pattern", "query", "glob", "regex"),
		todoSummary(m),
		mcpSummary(tool, m),
	}
	for _, s := range targets {
		if strings.TrimSpace(s) != "" {
			d.Target = s
			return d
		}
	}
	if compact := compactJSON(m); compact != "{}" {
		d.Target = compact
	}
	return d
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	for _, k := range keys {
		for mk, v := range m {
			if strings.EqualFold(mk, k) {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
	}
	return ""
}

func todoSummary(m map[string]any) string {
	xs, ok := m["todos"].([]any)
	if !ok || len(xs) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, x := range xs {
		todo, _ := x.(map[string]any)
		status, _ := todo["status"].(string)
		if status == "" {
			status = "unknown"
		}
		counts[status]++
	}
	var parts []string
	for _, status := range []string{"pending", "in_progress", "completed", "unknown"} {
		if n := counts[status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", status, n))
		}
	}
	return fmt.Sprintf("todos: %s", strings.Join(parts, ", "))
}

func mcpSummary(tool string, m map[string]any) string {
	low := strings.ToLower(tool)
	if !strings.Contains(low, "mcp") {
		return ""
	}
	name := firstString(m, "tool", "toolName", "name", "method")
	server := firstString(m, "server", "serverName")
	args := firstValue(m, "arguments", "args", "input", "params")
	var parts []string
	if server != "" {
		parts = append(parts, server)
	}
	if name != "" {
		parts = append(parts, name)
	}
	if args != nil {
		parts = append(parts, compactJSON(args))
	}
	return strings.Join(parts, " ")
}

func firstValue(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		return string(b)
	}
	return out.String()
}
