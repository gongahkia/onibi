package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/anomaly"
	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/pty"
)

func TestApprovalUnifiedDiffWriteScrubsBeforeDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	oldText := "token = \"oldsecret\"\nmode = \"old\"\n"
	newText := "token = \"newsecret\"\nmode = \"new\"\n"
	if err := os.WriteFile(path, []byte(oldText), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]any{
		"file_path": "config.env",
		"content":   newText,
	})
	if err != nil {
		t.Fatal(err)
	}
	diff := approvalUnifiedDiff(intake.Event{
		Tool:      "Write",
		CWD:       dir,
		InputJSON: string(input),
	})
	if diff == "" {
		t.Fatal("empty diff")
	}
	if strings.Contains(diff, "oldsecret") || strings.Contains(diff, "newsecret") {
		t.Fatalf("diff leaked secret: %s", diff)
	}
	if !strings.Contains(diff, "[REDACTED]") || !strings.Contains(diff, "+mode = \"new\"") {
		t.Fatalf("diff = %s", diff)
	}
}

func TestApprovalUnifiedDiffMultiEditAppliesFileEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("alpha\nalpha\nbeta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := `{"file_path":"doc.txt","edits":[{"old_string":"alpha","new_string":"omega","replace_all":true},{"oldString":"beta","newString":"gamma"}]}`
	diff := approvalUnifiedDiff(intake.Event{Tool: "MultiEdit", CWD: dir, InputJSON: input})
	if !strings.Contains(diff, "-alpha") || !strings.Contains(diff, "+omega") || !strings.Contains(diff, "+gamma") {
		t.Fatalf("diff = %s", diff)
	}
}

func TestApprovalDiffTextsMultiEditFallbackWithoutFile(t *testing.T) {
	oldText, newText, ok := approvalDiffTexts(intake.Event{
		Tool:      "MultiEdit",
		InputJSON: `{"edits":[{"old_string":"alpha","new_string":"omega"},{"oldString":"beta","newString":"gamma"}]}`,
	})
	if !ok || oldText != "alpha\nbeta" || newText != "omega\ngamma" {
		t.Fatalf("old=%q new=%q ok=%v", oldText, newText, ok)
	}
}

func TestApprovalUnifiedDiffNotebookEditCellSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nb.ipynb")
	raw := `{"cells":[{"id":"skip","source":"ignored"},{"id":"cell-2","source":["old ","source\n"]}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	input := `{"notebook_path":"nb.ipynb","cell_id":"cell-2","new_source":"new source\n"}`
	diff := approvalUnifiedDiff(intake.Event{Tool: "NotebookEdit", CWD: dir, InputJSON: input})
	if !strings.Contains(diff, "-old source") || !strings.Contains(diff, "+new source") {
		t.Fatalf("diff = %s", diff)
	}
}

func TestNotebookCellSourceRejectsInvalidSource(t *testing.T) {
	if got, ok := notebookCellSource(`{"cells":[{"id":"cell-1","source":["ok",7]}]}`, "cell-1"); ok || got != "" {
		t.Fatalf("got=%q ok=%v", got, ok)
	}
	if got, ok := notebookCellSource(`{"cells":[{"id":"cell-1","source":"ok"}]}`, "missing"); ok || got != "" {
		t.Fatalf("got=%q ok=%v", got, ok)
	}
}

func TestDemoApprovalRequestReturnsPending(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	approvalEvents, unsubscribe, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	resp, err := d.handleDemoApprovalRequest(t.Context(), intake.Event{
		Type:      intake.TypeDemoApproval,
		Session:   "demo-session",
		Agent:     "demo",
		Tool:      "Bash",
		InputJSON: `{"command":"echo onibi demo approval"}`,
		Action:    "request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "pending" || !strings.Contains(resp.Text, "demo approval requested:") {
		t.Fatalf("response = %#v", resp)
	}
	select {
	case ev := <-approvalEvents:
		if ev.Type != approval.EventRequested || ev.Approval.SessionID != "demo-session" || ev.Approval.Agent != "demo" {
			t.Fatalf("approval event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("approval event not published")
	}
}

func TestApprovalTimeoutEventAudited(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	s := NewSession("s1", "shell", "shell", nil, 0)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.handleEvent(t.Context(), intake.Event{
		Type:       intake.TypeApprovalTimeout,
		Session:    "s1",
		Managed:    true,
		Tool:       "Bash",
		ToolTarget: "sleep 400",
		InputJSON:  `{"command":"sleep 400"}`,
		Text:       "approval request timed out after 5m0s",
	}); err != nil {
		t.Fatal(err)
	}
	audit, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "approval.timeout" || !strings.Contains(audit[0].Detail, "tool=Bash") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestApprovalTimeoutEventCancelsPendingApproval(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	s := NewSession("s1", "shell", "shell", nil, 0)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	approvalID, ch, err := d.Queue.Request(t.Context(), "s1", "shell", "Bash", `{"command":"sleep 400"}`)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if err := d.handleEvent(t.Context(), intake.Event{
		Type:       intake.TypeApprovalTimeout,
		Session:    "s1",
		Managed:    true,
		ApprovalID: approvalID,
		Tool:       "Bash",
		ToolTarget: "sleep 400",
		InputJSON:  `{"command":"sleep 400"}`,
		Text:       "approval request timed out after 5m0s",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case dec := <-ch:
		if dec.Verdict != approval.VerdictCancel || dec.Reason != "approval request timed out after 5m0s" {
			t.Fatalf("decision = %#v", dec)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout decision not delivered")
	}
	ev := readTrustApprovalEvent(t, events)
	if ev.Type != approval.EventDecided || ev.Decision.Verdict != approval.VerdictCancel {
		t.Fatalf("approval event = %#v", ev)
	}
	a, err := d.Queue.Get(t.Context(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateCancelled || a.Reason != "approval request timed out after 5m0s" {
		t.Fatalf("approval = %#v", a)
	}
}

func TestRestorePendingApprovalsRetainsDisconnectedRestartedApproval(t *testing.T) {
	db := openDaemonTestDB(t)
	before := New(Options{DB: db})
	id, _, err := before.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"sleep 400"}`)
	if err != nil {
		t.Fatal(err)
	}
	before.Queue.DropWaiter(id)

	after := New(Options{DB: db})
	if err := after.RestorePendingApprovals(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err := after.Queue.Pending(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != id || pending[0].State != approval.StatePending {
		t.Fatalf("pending = %#v", pending)
	}
	audit, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "approval.recovered" || !strings.Contains(audit[0].Detail, id) {
		t.Fatalf("audit = %#v", audit)
	}
	res, err := after.Queue.DecideIdempotently(t.Context(), id, approval.VerdictApprove, "", "", 0)
	if err != nil || res.Delivered || res.Replayed || res.Decision.Verdict != approval.VerdictApprove {
		t.Fatalf("result=%#v err=%v", res, err)
	}
}

func TestRestorePendingApprovalsExpiresOverdueOnlyOnce(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(t.Context(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Unix(), id); err != nil {
		t.Fatal(err)
	}
	if err := d.RestorePendingApprovals(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.RestorePendingApprovals(t.Context()); err != nil {
		t.Fatal(err)
	}
	a, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateExpired {
		t.Fatalf("approval = %#v", a)
	}
	n, err := db.AuditCount(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("audit count = %d", n)
	}
}

func TestApprovalRequestConcurrentDecisionsOnlyOneWins(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	s := NewSession("s1", "claude", "claude", nil, 0)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	result := make(chan approvalResult, 1)
	go func() {
		resp, err := d.handleApprovalRequest(t.Context(), intake.Event{
			Type:      intake.TypeApprovalRequest,
			Session:   "s1",
			Managed:   true,
			Agent:     "claude",
			Tool:      "Bash",
			InputJSON: `{"command":"echo ok"}`,
		})
		result <- approvalResult{resp: resp, err: err}
	}()
	approvalEv := readTrustApprovalEvent(t, events)
	errs := make(chan error, 2)
	go func() {
		errs <- d.Queue.Decide(t.Context(), approvalEv.Approval.ID, approval.VerdictApprove, "", "", 1)
	}()
	go func() { errs <- d.Queue.Decide(t.Context(), approvalEv.Approval.ID, approval.VerdictDeny, "", "no", 2) }()
	var succeeded, alreadyDecided int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, approval.ErrAlreadyDecided):
			alreadyDecided++
		default:
			t.Fatal(err)
		}
	}
	if succeeded != 1 || alreadyDecided != 1 {
		t.Fatalf("succeeded=%d already_decided=%d", succeeded, alreadyDecided)
	}
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.resp.Decision != string(approval.VerdictApprove) && got.resp.Decision != string(approval.VerdictDeny) {
			t.Fatalf("response = %#v", got.resp)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not return")
	}
	a, err := d.Queue.Get(t.Context(), approvalEv.Approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateApproved && a.State != approval.StateDenied {
		t.Fatalf("approval state = %s", a.State)
	}
}

func TestResponseForDecisionDenyUsesProviderVerdict(t *testing.T) {
	resp := responseForDecision(approval.Decision{Verdict: approval.VerdictDeny, Reason: "owner denied"}, intake.Event{})
	if resp.Decision != string(approval.VerdictDeny) || resp.Reason != "owner denied" {
		t.Fatalf("response=%#v", resp)
	}
}

func TestClaudeApprovalDenyIsEnforcingAndAudited(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	if err := d.Registry.Add(NewSession("s1", "claude", "claude", nil, 0)); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	result := make(chan approvalResult, 1)
	payload := `{"command":"deploy --token raw-sensitive-value"}`
	go func() {
		resp, err := d.handleApprovalRequest(t.Context(), intake.Event{
			Type:      intake.TypeApprovalRequest,
			Session:   "s1",
			Managed:   true,
			Agent:     "claude",
			Tool:      "Bash",
			InputJSON: payload,
		})
		result <- approvalResult{resp: resp, err: err}
	}()
	requested := readTrustApprovalEvent(t, events)
	if err := d.Queue.Decide(t.Context(), requested.Approval.ID, approval.VerdictDeny, "", "owner denied", 9); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.resp.Decision != string(approval.VerdictDeny) || got.resp.Reason != "owner denied" {
			t.Fatalf("result=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not return")
	}
	rows, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Action == "approval.decided" {
			if row.PayloadHash == "" || strings.Contains(row.Detail, "raw-sensitive-value") {
				t.Fatalf("audit=%+v", row)
			}
			return
		}
	}
	t.Fatalf("approval decision audit missing: %+v", rows)
}

func TestApprovalRequestCancelsOnDaemonShutdown(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	s := NewSession("s1", "claude", "claude", nil, 0)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan approvalResult, 1)
	go func() {
		resp, err := d.handleApprovalRequest(ctx, intake.Event{
			Type:      intake.TypeApprovalRequest,
			Session:   "s1",
			Managed:   true,
			Agent:     "claude",
			Tool:      "Bash",
			InputJSON: `{"command":"echo ok"}`,
		})
		result <- approvalResult{resp: resp, err: err}
	}()
	approvalEv := readTrustApprovalEvent(t, events)
	cancel()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.resp.Decision != "cancelled" || got.resp.Reason != "daemon shutdown" {
			t.Fatalf("response = %#v", got.resp)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not return")
	}
	decidedEv := readTrustApprovalEvent(t, events)
	if decidedEv.Type != approval.EventDecided || decidedEv.Decision.Verdict != approval.VerdictCancel || decidedEv.Decision.Reason != "daemon shutdown" {
		t.Fatalf("approval event = %#v", decidedEv)
	}
	a, err := d.Queue.Get(t.Context(), approvalEv.Approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != approval.StateCancelled || a.Reason != "daemon shutdown" {
		t.Fatalf("approval = %#v", a)
	}
}

func TestAnomalyForkBombPausesAndResumesOnApprove(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	oldSignal := signalAnomalyProcessGroup
	var signals []syscall.Signal
	signalAnomalyProcessGroup = func(_ *Session, sig syscall.Signal) error {
		signals = append(signals, sig)
		return nil
	}
	t.Cleanup(func() { signalAnomalyProcessGroup = oldSignal })
	s := NewSession("s1", "claude", "claude", pty.NewVirtualHost(nil, nil, nil), 0)
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	approvalEvents, unsubApprovals, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubApprovals()
	webEvents, unsubWeb := d.Events.Subscribe()
	defer unsubWeb()
	result := make(chan approvalResult, 1)
	go func() {
		resp, err := d.handleApprovalRequest(t.Context(), intake.Event{
			Type:      intake.TypeApprovalRequest,
			Session:   "s1",
			Managed:   true,
			Agent:     "claude",
			Tool:      "Bash",
			InputJSON: `{"command":":(){ :|:& };:"}`,
		})
		result <- approvalResult{resp: resp, err: err}
	}()
	approvalEv := readTrustApprovalEvent(t, approvalEvents)
	if approvalEv.Type != approval.EventRequested {
		t.Fatalf("approval event = %#v", approvalEv)
	}
	webEv := readTrustWebEvent(t, webEvents)
	if webEv.Type != anomalyRequestedEvent {
		t.Fatalf("web event = %#v", webEv)
	}
	payload, ok := webEv.Payload.(anomalyEvent)
	if !ok || payload.ApprovalID != approvalEv.Approval.ID || payload.RuleName != anomaly.RuleForkBomb || !payload.Paused || !strings.Contains(payload.Evidence, "fork bomb pattern") {
		t.Fatalf("payload = %#v", webEv.Payload)
	}
	if len(signals) != 1 || signals[0] != syscall.SIGSTOP {
		t.Fatalf("signals before approve = %#v", signals)
	}
	if err := d.Queue.Decide(t.Context(), approvalEv.Approval.ID, approval.VerdictApprove, "", "", 0); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.resp.Decision != string(approval.VerdictApprove) {
			t.Fatalf("response = %#v", got.resp)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not return")
	}
	if len(signals) != 2 || signals[1] != syscall.SIGCONT {
		t.Fatalf("signals after approve = %#v", signals)
	}
}
