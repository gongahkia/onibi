package daemon

import (
	"encoding/json"
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
