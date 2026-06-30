package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/intake"
)

func (d *Daemon) handleApprovalRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	s, reason := d.sessionForEvent(ev)
	if s == nil {
		d.auditIgnoredHook(ctx, "approval.ignored", ev, reason)
		return intake.Response{Decision: "cancelled", Reason: "unmanaged or unknown Onibi session"}, nil
	}
	ev.Session = s.ID
	d.appendEventOutput(s, ev)
	unifiedDiff := approvalUnifiedDiff(ev)
	approvalID, ch, err := d.Queue.Request(ctx, ev.Session, ev.Agent, ev.Tool, ev.InputJSON, unifiedDiff)
	if err != nil {
		return intake.Response{Decision: "cancelled", Reason: err.Error()}, nil
	}
	a, getErr := d.Queue.Get(ctx, approvalID)
	if getErr != nil {
		return intake.Response{Decision: "cancelled", Reason: getErr.Error()}, nil
	}
	d.noteAnomaly(ctx, "approval.request")
	if isHighRiskApproval(a) {
		d.noteAnomaly(ctx, "approval.high_risk")
	}
	d.audit(ctx, "approval.request", ev.Session, ev.InputJSON, 0, fmt.Sprintf("tool=%s id=%s", ev.Tool, approvalID))
	select {
	case dec := <-ch:
		return responseForDecision(dec, ev), nil
	case <-ctx.Done():
		_ = d.Queue.Cancel(context.Background(), approvalID, "daemon shutdown")
		return intake.Response{Decision: "cancelled", Reason: "daemon shutdown"}, nil
	}
}

func (d *Daemon) handleDemoApprovalRequest(ctx context.Context, ev intake.Event) (intake.Response, error) {
	approvalID, ch, err := d.startDemoApproval(ctx, ev)
	if err != nil {
		return intake.Response{}, err
	}
	select {
	case dec := <-ch:
		return responseForDecision(dec, ev), nil
	case <-ctx.Done():
		_ = d.Queue.Cancel(context.Background(), approvalID, "demo approval cancelled")
		return intake.Response{Decision: "cancelled", Reason: "demo approval cancelled"}, nil
	}
}

func (d *Daemon) startDemoApproval(ctx context.Context, ev intake.Event) (string, <-chan approval.Decision, error) {
	tool := strings.TrimSpace(ev.Tool)
	if tool == "" {
		tool = "Bash"
	}
	inputJSON := strings.TrimSpace(ev.InputJSON)
	if inputJSON == "" {
		inputJSON = `{"command":"echo onibi demo approval"}`
	}
	agent := strings.TrimSpace(ev.Agent)
	if agent == "" {
		agent = "demo"
	}
	sessionID := strings.TrimSpace(ev.Session)
	if sessionID == "" {
		sessionID = "demo"
	}
	approvalID, ch, err := d.Queue.Request(ctx, sessionID, agent, tool, inputJSON)
	if err != nil {
		return "", nil, err
	}
	d.audit(ctx, "approval.demo", sessionID, inputJSON, 0, "tool="+tool+" id="+approvalID)
	return approvalID, ch, nil
}

func (d *Daemon) RestorePendingApprovals(ctx context.Context) error {
	_ = ctx
	return nil
}

func responseForDecision(dec approval.Decision, ev intake.Event) intake.Response {
	switch dec.Verdict {
	case approval.VerdictApprove:
		return intake.Response{Decision: string(approval.VerdictApprove)}
	case approval.VerdictEdit:
		return intake.Response{Decision: string(approval.VerdictEdit), UpdatedInput: string(dec.UpdatedInput)}
	case approval.VerdictDeny:
		return intake.Response{Decision: "denied", Reason: dec.Reason}
	case approval.VerdictExpire:
		return intake.Response{Decision: "expired", Reason: dec.Reason}
	default:
		return intake.Response{Decision: "cancelled", Reason: dec.Reason}
	}
}

func isHighRiskApproval(a *approval.Approval) bool {
	if a == nil {
		return false
	}
	return approval.ClassifyRisk(a.Tool, a.InputJSON).Level == "high"
}

func approvalUnifiedDiff(ev intake.Event) string {
	if !approvalDiffTool(ev.Tool) {
		return ""
	}
	oldText, newText, ok := approvalDiffTexts(ev)
	if !ok || oldText == newText {
		return ""
	}
	diff, err := unifiedDiff(approval.Scrub(oldText), approval.Scrub(newText))
	if err != nil {
		return ""
	}
	return diff
}

func approvalDiffTool(tool string) bool {
	switch strings.TrimSpace(tool) {
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return true
	default:
		return false
	}
}

func approvalDiffTexts(ev intake.Event) (string, string, bool) {
	var input map[string]any
	if err := json.Unmarshal([]byte(ev.InputJSON), &input); err != nil {
		return "", "", false
	}
	path := approvalInputPath(ev.Tool, input)
	oldText, oldOK := readApprovalFile(resolveApprovalPath(ev.CWD, path))
	switch strings.TrimSpace(ev.Tool) {
	case "Write":
		content, ok := inputString(input, "content")
		if !ok {
			return "", "", false
		}
		return oldText, content, true
	case "Edit":
		return editDiffTexts(oldText, oldOK, input)
	case "MultiEdit":
		return multiEditDiffTexts(oldText, oldOK, input)
	case "NotebookEdit":
		return notebookDiffTexts(oldText, oldOK, input)
	default:
		return "", "", false
	}
}

func editDiffTexts(oldText string, oldOK bool, input map[string]any) (string, string, bool) {
	oldString, okOld := inputString(input, "old_string", "oldString")
	newString, okNew := inputString(input, "new_string", "newString")
	if !okOld || !okNew {
		return "", "", false
	}
	if oldOK && oldString != "" && strings.Contains(oldText, oldString) {
		n := 1
		if inputBool(input, "replace_all", "replaceAll") {
			n = -1
		}
		return oldText, strings.Replace(oldText, oldString, newString, n), true
	}
	return oldString, newString, true
}

func multiEditDiffTexts(oldText string, oldOK bool, input map[string]any) (string, string, bool) {
	rawEdits, ok := input["edits"].([]any)
	if !ok || len(rawEdits) == 0 {
		return "", "", false
	}
	current := oldText
	changed := false
	var oldParts, newParts []string
	for _, raw := range rawEdits {
		edit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		oldString, okOld := inputString(edit, "old_string", "oldString")
		newString, okNew := inputString(edit, "new_string", "newString")
		if !okOld || !okNew {
			continue
		}
		oldParts = append(oldParts, oldString)
		newParts = append(newParts, newString)
		if oldOK && oldString != "" && strings.Contains(current, oldString) {
			n := 1
			if inputBool(edit, "replace_all", "replaceAll") {
				n = -1
			}
			next := strings.Replace(current, oldString, newString, n)
			if next != current {
				current = next
				changed = true
			}
		}
	}
	if oldOK && changed {
		return oldText, current, true
	}
	if len(oldParts) == 0 {
		return "", "", false
	}
	return strings.Join(oldParts, "\n"), strings.Join(newParts, "\n"), true
}

func notebookDiffTexts(oldText string, oldOK bool, input map[string]any) (string, string, bool) {
	newSource, ok := inputString(input, "new_source", "newSource", "source")
	if !ok {
		return "", "", false
	}
	cellID, _ := inputString(input, "cell_id", "cellId")
	if oldOK {
		if oldSource, ok := notebookCellSource(oldText, cellID); ok {
			return oldSource, newSource, true
		}
	}
	return "", newSource, true
}

func notebookCellSource(raw, cellID string) (string, bool) {
	var doc struct {
		Cells []struct {
			ID     string `json:"id"`
			Source any    `json:"source"`
		} `json:"cells"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return "", false
	}
	for _, cell := range doc.Cells {
		if cellID != "" && cell.ID != cellID {
			continue
		}
		return normalizeNotebookSource(cell.Source)
	}
	return "", false
}

func normalizeNotebookSource(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case []any:
		var b strings.Builder
		for _, part := range x {
			s, ok := part.(string)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		return b.String(), true
	default:
		return "", false
	}
}

func approvalInputPath(tool string, input map[string]any) string {
	if strings.TrimSpace(tool) == "NotebookEdit" {
		if path, ok := inputString(input, "notebook_path", "notebookPath", "file_path", "filePath", "path"); ok {
			return path
		}
	}
	path, _ := inputString(input, "file_path", "filePath", "filepath", "path")
	return path
}

func resolveApprovalPath(cwd, path string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return filepath.Clean(path)
	}
	return filepath.Join(cwd, path)
}

func readApprovalFile(path string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func inputString(m map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if v, ok := m[key].(string); ok {
			return v, true
		}
	}
	for _, key := range keys {
		for got, v := range m {
			if strings.EqualFold(got, key) {
				if s, ok := v.(string); ok {
					return s, true
				}
			}
		}
	}
	return "", false
}

func inputBool(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if v, ok := m[key].(bool); ok {
			return v
		}
	}
	for _, key := range keys {
		for got, v := range m {
			if strings.EqualFold(got, key) {
				if b, ok := v.(bool); ok {
					return b
				}
			}
		}
	}
	return false
}

func unifiedDiff(oldText, newText string) (string, error) {
	dir, err := os.MkdirTemp("", "onibi-diff-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	oldPath := filepath.Join(dir, "old")
	newPath := filepath.Join(dir, "new")
	if err := os.WriteFile(oldPath, []byte(oldText), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(newPath, []byte(newText), 0o600); err != nil {
		return "", err
	}
	out, err := exec.Command("diff", "-u", oldPath, newPath).CombinedOutput()
	if err == nil {
		return string(out), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return string(out), nil
	}
	return "", err
}
