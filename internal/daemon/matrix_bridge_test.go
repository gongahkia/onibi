//go:build !onibi_remote

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestMatrixReactionApprovesPendingApproval(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example", OwnerUserID: "@owner:example"}})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"pwd"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.storeMatrixApprovalEvent(t.Context(), "$approval", id)
	var sent []string
	c := matrixTestClient(t, &sent)
	d.handleMatrixEvent(t.Context(), c, matrix.Event{
		EventID: "$reaction",
		Type:    "m.reaction",
		Sender:  "@owner:example",
		Content: json.RawMessage(`{"m.relates_to":{"rel_type":"m.annotation","event_id":"$approval","key":"✅"}}`),
	})
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("approval state = %s", got.State)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "Approval "+id+": approved") {
		t.Fatalf("sent = %#v", sent)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.reaction") || !auditHas(audit, "approval.decided") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMatrixTextInRoutesToPTYAndAuditsTail(t *testing.T) {
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
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example", OwnerUserID: "@owner:example"}})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	var sent []string
	c := matrixTestClient(t, &sent)
	d.handleMatrixEvent(t.Context(), c, matrix.Event{
		EventID: "$msg",
		Type:    "m.room.message",
		Sender:  "@owner:example",
		Content: json.RawMessage(`{"msgtype":"m.text","body":"pwd"}`),
	})
	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "-l", "--", "pwd") {
		t.Fatalf("missing tmux send: %#v", r.calls)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "/tmp/onibi") {
		t.Fatalf("sent = %#v", sent)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.text_in") || !auditHas(audit, "provider.matrix.tail_chunk") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMatrixReactionVerdict(t *testing.T) {
	if matrixReactionVerdict("✅") != approval.VerdictApprove || matrixReactionVerdict("👍") != approval.VerdictApprove {
		t.Fatal("approve reaction mapping failed")
	}
	if matrixReactionVerdict("❌") != approval.VerdictDeny || matrixReactionVerdict("👎") != approval.VerdictDeny {
		t.Fatal("deny reaction mapping failed")
	}
	if matrixReactionVerdict("🙂") != "" {
		t.Fatal("unknown reaction mapped to verdict")
	}
}

func matrixTestClient(t *testing.T, sent *[]string) *matrix.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/send/m.room.message/") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var msg matrix.RoomMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Fatal(err)
		}
		*sent = append(*sent, msg.Body)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(matrix.SendResponse{EventID: "$sent"}); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := matrix.New(srv.URL, "tok")
	c.TxnID = func() string { return "txn" }
	return c
}

func auditHas(entries []store.AuditEntry, action string) bool {
	for _, entry := range entries {
		if entry.Action == action {
			return true
		}
	}
	return false
}
