package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/budget"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/trust"
	"github.com/gongahkia/onibi/internal/web"
)

func TestDaemonPublishesClaudeCostEvent(t *testing.T) {
	db := openDaemonTestDB(t)
	base := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := budget.ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	providerSession := "claude-session"
	transcript := filepath.Join(base, "projects", key, providerSession+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte(`{"message":{"model":"claude-sonnet-4-5","usage":{"input_tokens":12,"output_tokens":4}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Budget: budget.NewClaudeParser(base)})
	events, unsub := d.Events.Subscribe()
	defer unsub()
	s := NewSession("s1", "claude", "claude", nil, 0)
	s.CWD = cwd
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.handleEvent(t.Context(), intake.Event{
		Type:              intake.TypeAgentDone,
		Session:           "s1",
		Managed:           true,
		Agent:             "claude",
		CWD:               cwd,
		ProviderSessionID: providerSession,
	}); err != nil {
		t.Fatal(err)
	}
	ev := readCostWebEvent(t, events)
	cost, ok := ev.Payload.(budget.CostEvent)
	if !ok {
		t.Fatalf("payload = %#v", ev.Payload)
	}
	if cost.SessionID != "s1" || cost.ProviderSessionID != providerSession || cost.InputTokens != 12 || cost.OutputTokens != 4 || cost.Model != "claude-sonnet-4-5" {
		t.Fatalf("cost = %#v", cost)
	}
	view, ok, err := d.SessionCost(t.Context(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || view.SessionID != "s1" || view.TotalTokens != 16 || view.DailyTokens != 16 {
		t.Fatalf("view = %#v ok=%v", view, ok)
	}
}

func TestBudgetWarningSuppressesTrustAutoApprove(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	root := t.TempDir()
	writeBudgetPolicy(t, root, "[session]\nmax_tokens = 1\non_overrun = \"interrupt\"\n")
	if err := os.MkdirAll(filepath.Join(root, ".onibi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".onibi", "trust.toml"), []byte(`[[rule]]
effect = "auto_approve"
expires = "never"
[rule.match]
tool = "Edit"
path = "src/**"
agent = "claude"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := trust.NewWatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.AddRoot(root); err != nil {
		t.Fatal(err)
	}
	d.Trust = w
	s := NewSession("s1", "claude", "claude", nil, 0)
	s.CWD = root
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	events, unsub, err := d.Queue.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	result := make(chan approvalResult, 1)
	go func() {
		resp, err := d.handleApprovalRequest(t.Context(), intake.Event{
			Type:      intake.TypeApprovalRequest,
			Session:   "s1",
			Managed:   true,
			Agent:     "claude",
			Tool:      "Edit",
			InputJSON: `{"file_path":"src/main.go","old_string":"aaaaaaaa","new_string":"bbbbbbbb"}`,
		})
		result <- approvalResult{resp: resp, err: err}
	}()
	ev := readTrustApprovalEvent(t, events)
	if ev.Type != approval.EventRequested || ev.Approval.BudgetWarn == nil {
		t.Fatalf("event = %#v", ev)
	}
	if ev.Approval.BudgetWarn.Scope != "session" || ev.Approval.BudgetWarn.OnOverrun != "interrupt" {
		t.Fatalf("warning = %#v", ev.Approval.BudgetWarn)
	}
	if err := d.Queue.Decide(t.Context(), ev.Approval.ID, approval.VerdictApprove, "", "", 0); err != nil {
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
}

func TestBudgetOverrunInterruptsSession(t *testing.T) {
	db := openDaemonTestDB(t)
	base := t.TempDir()
	root := t.TempDir()
	writeBudgetPolicy(t, root, "[session]\nmax_tokens = 10\n")
	writeClaudeUsage(t, base, root, "claude-session", "claude-sonnet-4-6", 8, 5)
	var writes [][]byte
	host := pty.NewVirtualHost(func(p []byte) (int, error) {
		writes = append(writes, append([]byte(nil), p...))
		return len(p), nil
	}, nil, nil)
	d := New(Options{DB: db, Budget: budget.NewClaudeParser(base)})
	s := NewSession("s1", "claude", "claude", host, 0)
	s.CWD = root
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.handleEvent(t.Context(), intake.Event{
		Type:              intake.TypeAgentDone,
		Session:           "s1",
		Managed:           true,
		Agent:             "claude",
		CWD:               root,
		ProviderSessionID: "claude-session",
	}); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || string(writes[0]) != string([]byte{3}) {
		t.Fatalf("writes = %#v", writes)
	}
	view, err := d.BudgetView(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Sessions) != 1 {
		t.Fatalf("view = %#v", view)
	}
	usage := view.Sessions[0]
	if usage.TotalTokens != 13 || !usage.CostKnown || usage.RemainingTokens == nil || *usage.RemainingTokens != -3 || usage.TotalUSD <= 0 {
		t.Fatalf("usage = %#v", usage)
	}
	if view.Daily.TotalTokens != 13 || !view.Daily.CostKnown || view.Daily.TotalUSD <= 0 || view.Daily.LimitTokens != nil {
		t.Fatalf("daily = %#v", view.Daily)
	}
}

func TestBudgetOverrunKillsSession(t *testing.T) {
	db := openDaemonTestDB(t)
	base := t.TempDir()
	root := t.TempDir()
	writeBudgetPolicy(t, root, "[session]\nmax_tokens = 10\non_overrun = \"kill\"\n")
	writeClaudeUsage(t, base, root, "claude-session", "claude-sonnet-4-6", 8, 5)
	closed := false
	host := pty.NewVirtualHost(func(p []byte) (int, error) {
		t.Fatalf("unexpected write: %#v", p)
		return 0, nil
	}, func() error {
		closed = true
		return nil
	}, nil)
	d := New(Options{DB: db, Budget: budget.NewClaudeParser(base)})
	s := NewSession("s1", "claude", "claude", host, 0)
	s.CWD = root
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.handleEvent(t.Context(), intake.Event{
		Type:              intake.TypeAgentDone,
		Session:           "s1",
		Managed:           true,
		Agent:             "claude",
		CWD:               root,
		ProviderSessionID: "claude-session",
	}); err != nil {
		t.Fatal(err)
	}
	if !closed || !s.Ended() {
		t.Fatalf("closed=%v ended=%v", closed, s.Ended())
	}
}

func TestPiBudgetOverrunInterruptsSession(t *testing.T) {
	db := openDaemonTestDB(t)
	base := t.TempDir()
	root := t.TempDir()
	writeBudgetPolicy(t, root, "[session]\nmax_tokens = 10\n")
	providerSessionID := "a6d1f8f3-75d4-4b2f-93d2-1ed8ddda89f8"
	writePiUsage(t, base, providerSessionID, "openai/gpt-5", 8, 5)
	var writes [][]byte
	host := pty.NewVirtualHost(func(p []byte) (int, error) {
		writes = append(writes, append([]byte(nil), p...))
		return len(p), nil
	}, nil, nil)
	d := New(Options{DB: db, PiBudget: budget.NewPiParser(base)})
	s := NewSession("s1", "pi", "pi", host, 0)
	s.CWD = root
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := d.handleEvent(t.Context(), intake.Event{Type: intake.TypeAgentDone, Session: "s1", Managed: true, Agent: "pi", CWD: root, ProviderSessionID: providerSessionID}); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || string(writes[0]) != string([]byte{3}) {
		t.Fatalf("writes = %#v", writes)
	}
	view, err := d.BudgetView(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Sessions) != 1 || view.Sessions[0].Agent != "pi" || view.Sessions[0].TotalTokens != 13 {
		t.Fatalf("view = %#v", view)
	}
}

func TestFleetBudgetReportIncludesOnlyCertifiedAgentsAndMeasurementState(t *testing.T) {
	d := New(Options{DB: openDaemonTestDB(t)})
	root := t.TempDir()
	writeBudgetPolicy(t, root, "[global]\nmax_tokens_per_day = 20\n[session]\nmax_tokens = 10\non_overrun = \"kill\"\n")
	for _, agent := range []string{"claude", "codex", "pi", "shell"} {
		s := NewSession("session-"+agent, agent, agent, nil, 0)
		s.CWD = root
		if err := d.Registry.Add(s); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Now().UTC().Format("2006-01-02")
	d.budgetDaily[day] = 20
	d.budgetCosts["session-claude"] = budget.CostEvent{SessionID: "session-claude", TotalInputTokens: 8, TotalOutputTokens: 7}
	d.budgetCosts["session-pi"] = budget.CostEvent{SessionID: "session-pi", TotalInputTokens: 3, TotalOutputTokens: 2}
	report := d.FleetBudgetReport()
	if report.Date != day || report.DailyTokens != 20 || report.GlobalLimit != 20 || report.OnOverrun != "kill" || len(report.Sessions) != 3 {
		t.Fatalf("report=%#v", report)
	}
	for _, session := range report.Sessions {
		if session.Limit != 10 || session.OnOverrun != "kill" || (session.Agent == "claude" && (!session.Measured || session.Tokens != 15)) || (session.Agent == "pi" && (!session.Measured || session.Tokens != 5)) || (session.Agent == "codex" && (session.Measured || session.Tokens != 0)) {
			t.Fatalf("session=%#v", session)
		}
	}
}

func readCostWebEvent(t *testing.T, events <-chan web.Event) web.Event {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(time.Second):
		t.Fatal("cost event not delivered")
		return web.Event{}
	}
}

type approvalResult struct {
	resp intake.Response
	err  error
}

func writeBudgetPolicy(t *testing.T, root, body string) {
	t.Helper()
	path := budget.PolicyPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeClaudeUsage(t *testing.T, base, cwd, providerSession, model string, input, output int64) {
	t.Helper()
	key, err := budget.ClaudeProjectKey(cwd)
	if err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(base, "projects", key, providerSession+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"message":{"model":%q,"usage":{"input_tokens":%d,"output_tokens":%d}}}`+"\n", model, input, output)
	if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePiUsage(t *testing.T, base, providerSession, model string, input, output int64) {
	t.Helper()
	transcript := filepath.Join(base, "--tmp-repo--", "2026-07-15T00-00-00-000Z_"+providerSession+".jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"type":"session","version":3,"id":%q}`+"\n"+`{"type":"message","message":{"role":"assistant","model":%q,"usage":{"input":%d,"output":%d}}}`+"\n", providerSession, model, input, output)
	if err := os.WriteFile(transcript, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
