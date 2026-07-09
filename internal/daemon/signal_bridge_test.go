//go:build !onibi_remote

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/approval"
	signalapi "github.com/gongahkia/onibi/internal/signal"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestSignalReactionApprovesPendingApproval(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Signal: SignalOptions{Account: "+111", Recipients: []string{"+222"}}})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"pwd"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.signalApprovals[123] = id
	var sent []string
	c := signalTestClient(t, &sent)
	err = d.handleSignalEvent(t.Context(), c, signalapi.Event{Envelope: signalapi.Envelope{
		Source:      "+222",
		DataMessage: &signalapi.DataMessage{Reaction: json.RawMessage(`{"emoji":"👍","targetSentTimestamp":123}`)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("approval state = %s", got.State)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "Approval "+id+" approve.") {
		t.Fatalf("sent = %#v", sent)
	}
	audit, err := db.AuditRecent(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "approval.decided" {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestSignalTextInRoutesToPTYAndReturnsTail(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{
		nil,
		nil,
		[]byte("$ pwd\n/tmp/onibi\n"),
		[]byte("$ pwd\n/tmp/onibi\n"),
		[]byte("$ pwd\n/tmp/onibi\n"),
	}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Signal: SignalOptions{Account: "+111", Recipients: []string{"+222"}}})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	var sent []string
	c := signalTestClient(t, &sent)
	err := d.handleSignalEvent(t.Context(), c, signalapi.Event{Envelope: signalapi.Envelope{
		Source: "+222",
		DataMessage: &signalapi.DataMessage{
			Message: "pwd",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "-l", "--", "pwd") {
		t.Fatalf("missing tmux send: %#v", r.calls)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "/tmp/onibi") {
		t.Fatalf("sent = %#v", sent)
	}
	audit, err := db.AuditRecent(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) != 2 || audit[0].Action != "provider.signal.tail_chunk" || audit[1].Action != "provider.signal.text_in" {
		t.Fatalf("audit = %#v", audit)
	}
}

func signalTestClient(t *testing.T, sent *[]string) *signalapi.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/rpc":
			var req struct {
				Params map[string]any `json:"params"`
				ID     any            `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			*sent = append(*sent, req.Params["message"].(string))
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": signalapi.SendResult{Timestamp: int64(len(*sent))}}); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return signalapi.New(srv.URL, "+111")
}

func TestSignalEmojiVerdict(t *testing.T) {
	if signalEmojiVerdict("👍") != approval.VerdictApprove || signalEmojiVerdict("✅") != approval.VerdictApprove {
		t.Fatal("approve emoji mapping failed")
	}
	if signalEmojiVerdict("👎") != approval.VerdictDeny || signalEmojiVerdict("❌") != approval.VerdictDeny {
		t.Fatal("deny emoji mapping failed")
	}
	if signalEmojiVerdict("🙂") != "" {
		t.Fatal("unexpected neutral verdict")
	}
}

func TestSignalAllowedSourceDefaultsToRecipient(t *testing.T) {
	d := New(Options{Signal: SignalOptions{Recipients: []string{"+222"}}})
	if !d.signalAllowedSource(signalapi.Envelope{Source: "+222"}) {
		t.Fatal("recipient source should be allowed")
	}
	if d.signalAllowedSource(signalapi.Envelope{Source: "+333"}) {
		t.Fatal("unexpected source allowed")
	}
}
