package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/pushover"
)

func TestPushoverReceiptAudit(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages.json":
			_ = json.NewEncoder(w).Encode(pushover.MessageResponse{Status: 1, Receipt: "r1"})
		case r.URL.Path == "/receipts/r1.json":
			_ = json.NewEncoder(w).Encode(pushover.Receipt{Status: 1, Acknowledged: 1})
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := pushover.New("app", "user")
	c.BaseURL = srv.URL
	a := &approval.Approval{ID: "a1", SessionID: "s1", Agent: "claude", Tool: "Bash", InputJSON: `{"command":"ls"}`}
	d.sendPushoverApproval(t.Context(), c, a)
	deadline := time.After(2 * time.Second)
	for {
		rows, err := db.AuditRecent(t.Context(), 10)
		if err != nil {
			t.Fatal(err)
		}
		var actions []string
		for _, row := range rows {
			actions = append(actions, row.Action)
		}
		if strings.Contains(strings.Join(actions, "\n"), "notify.pushover.receipt.acknowledged") {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("audit actions = %#v", actions)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
