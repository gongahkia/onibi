//go:build !onibi_remote

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/pushover"
	"github.com/gongahkia/onibi/internal/web"
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
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	a, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
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
		joined := strings.Join(actions, "\n")
		if strings.Contains(joined, "notify.pushover.receipt.acknowledged") && strings.Contains(joined, "notify.pushover.approve") {
			got, err := d.Queue.Get(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != approval.StateApproved {
				t.Fatalf("state = %s", got.State)
			}
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

func TestNtfyActionsRetryAudit(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Ntfy: NtfyOptions{ActionBaseURL: "https://onibi.example"}})
	signer, err := web.NewActionSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	d.notifyActionSigner = signer
	topic := "A1b2C3d4E5f6G7h8I9j0"
	actions := make(chan string, 1)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "try again", http.StatusBadGateway)
			return
		}
		actions <- r.Header.Get("X-Actions")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go d.runNtfyNotifier(ctx, ntfy.New(srv.URL, topic, ""))
	select {
	case got := <-actions:
		if !strings.Contains(got, "http, Approve, https://onibi.example/ntfy/approval/"+id+"/approve") || !strings.Contains(got, "http, Deny, https://onibi.example/ntfy/approval/"+id+"/deny") {
			t.Fatalf("actions = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ntfy publish")
	}
	deadline := time.After(2 * time.Second)
	for {
		rows, err := db.AuditAll(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, row := range rows {
			seen[row.Action] = true
		}
		if seen["notify.ntfy.retry"] && seen["notify.ntfy.sent"] {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("audit rows = %#v", rows)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestGotifyActionURLRetryAudit(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Gotify: GotifyOptions{ActionBaseURL: "https://onibi.example"}})
	signer, err := web.NewActionSigner([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	d.notifyActionSigner = signer
	gotURLs := make(chan string, 1)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "try again", http.StatusBadGateway)
			return
		}
		var msg gotify.Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Fatal(err)
		}
		notification := msg.Extras["client::notification"].(map[string]any)
		click := notification["click"].(map[string]any)
		gotURLs <- click["url"].(string)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go d.runGotifyNotifier(ctx, gotify.New(srv.URL, "app", "client"))
	select {
	case got := <-gotURLs:
		if !strings.Contains(got, "https://onibi.example/gotify/approval/"+id) {
			t.Fatalf("url = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for gotify send")
	}
	deadline := time.After(2 * time.Second)
	for {
		rows, err := db.AuditAll(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, row := range rows {
			seen[row.Action] = true
		}
		if seen["notify.gotify.retry"] && seen["notify.gotify.sent"] {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("audit rows = %#v", rows)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
