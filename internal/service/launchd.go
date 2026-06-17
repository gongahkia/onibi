package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (m *Manager) installLaunchd(ctx context.Context) error {
	path, err := m.ServicePath()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, []byte(m.launchdPlist()), 0o644); err != nil {
		return fmt.Errorf("write launchagent: %w", err)
	}
	domain := fmt.Sprintf("gui/%d", m.UID)
	_, _ = m.Runner.Run(ctx, "launchctl", "bootout", domain, path)
	if out, err := m.Runner.Run(ctx, "launchctl", "bootstrap", domain, path); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) uninstallLaunchd(ctx context.Context) error {
	path, err := m.ServicePath()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", m.UID)
	_, _ = m.Runner.Run(ctx, "launchctl", "bootout", domain, path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) launchdStatus(ctx context.Context) Status {
	path, err := m.ServicePath()
	if err != nil {
		return Status{Detail: err.Error()}
	}
	st := Status{Path: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
	} else if !os.IsNotExist(err) {
		st.Detail = err.Error()
	}
	out, err := m.Runner.Run(ctx, "launchctl", "print", fmt.Sprintf("gui/%d/%s", m.UID, Label))
	if err == nil {
		st.Running = true
		st.Detail = firstLine(out)
	} else if len(out) > 0 {
		st.Detail = strings.TrimSpace(string(out))
	}
	return st
}

func (m *Manager) launchdPlist() string {
	stdout := filepath.Join(m.Paths.LogDir, "onibi.out.log")
	stderr := filepath.Join(m.Paths.LogDir, "onibi.err.log")
	val := map[string]any{
		"Label":            Label,
		"ProgramArguments": []string{m.Executable, "run"},
		"RunAtLoad":        true,
		"KeepAlive":        map[string]any{"Crashed": true},
		"ProcessType":      "Interactive",
		"WorkingDirectory": m.Paths.StateDir,
		"EnvironmentVariables": map[string]string{
			"PATH": launchdPATH(),
		},
		"StandardOutPath":   stdout,
		"StandardErrorPath": stderr,
	}
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	writePlistDict(&b, val)
	b.WriteString(`</plist>` + "\n")
	return b.String()
}

func writePlistDict(b *bytes.Buffer, vals map[string]any) {
	b.WriteString("<dict>\n")
	keys := []string{"Label", "ProgramArguments", "RunAtLoad", "KeepAlive", "ProcessType", "WorkingDirectory", "EnvironmentVariables", "StandardOutPath", "StandardErrorPath"}
	for _, k := range keys {
		v, ok := vals[k]
		if !ok {
			continue
		}
		fmt.Fprintf(b, "  <key>%s</key>\n", k)
		writePlistValue(b, v, "  ")
	}
	b.WriteString("</dict>\n")
}

func writePlistValue(b *bytes.Buffer, v any, indent string) {
	switch x := v.(type) {
	case string:
		fmt.Fprintf(b, "%s<string>%s</string>\n", indent, xmlEscape(x))
	case bool:
		if x {
			fmt.Fprintf(b, "%s<true/>\n", indent)
		} else {
			fmt.Fprintf(b, "%s<false/>\n", indent)
		}
	case []string:
		fmt.Fprintf(b, "%s<array>\n", indent)
		for _, s := range x {
			fmt.Fprintf(b, "%s  <string>%s</string>\n", indent, xmlEscape(s))
		}
		fmt.Fprintf(b, "%s</array>\n", indent)
	case map[string]string:
		fmt.Fprintf(b, "%s<dict>\n", indent)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "%s  <key>%s</key>\n", indent, xmlEscape(k))
			fmt.Fprintf(b, "%s  <string>%s</string>\n", indent, xmlEscape(x[k]))
		}
		fmt.Fprintf(b, "%s</dict>\n", indent)
	case map[string]any:
		fmt.Fprintf(b, "%s<dict>\n", indent)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(b, "%s  <key>%s</key>\n", indent, xmlEscape(k))
			writePlistValue(b, x[k], indent+"  ")
		}
		fmt.Fprintf(b, "%s</dict>\n", indent)
	}
}

func launchdPATH() string {
	parts := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/opt/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	if env := strings.TrimSpace(os.Getenv("PATH")); env != "" {
		parts = append(strings.Split(env, ":"), parts...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return strings.Join(out, ":")
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
