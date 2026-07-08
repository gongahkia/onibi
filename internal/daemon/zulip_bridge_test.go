//go:build !onibi_remote

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/zulip"
)

func TestZulipTopicMapsSession(t *testing.T) {
	d := New(Options{})
	if got := d.zulipTopicForSession("s1"); got != "onibi-s1" {
		t.Fatalf("topic = %q", got)
	}
	if got := d.zulipSessionFromTopic("onibi-s1"); got != "s1" {
		t.Fatalf("session = %q", got)
	}
	d.Zulip.TopicPrefix = "sess/"
	if got := d.zulipTopicForSession("s2"); got != "sess/s2" {
		t.Fatalf("custom topic = %q", got)
	}
	if got := d.zulipSessionFromTopic("other"); got != "" {
		t.Fatalf("unmapped session = %q", got)
	}
}

func TestZulipMessageApprovesInTopic(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db})
	d.Zulip = ZulipOptions{Email: "onibi-bot@example.com", OwnerEmail: "owner@example.com", Stream: "onibi"}
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"pwd"}`)
	if err != nil {
		t.Fatal(err)
	}
	posted := make(chan url.Values, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/messages" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		posted <- r.Form
		if err := json.NewEncoder(w).Encode(map[string]any{"id": 10}); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()
	c := zulip.New(srv.URL, d.Zulip.Email, "key")
	err = d.handleZulipEvent(t.Context(), c, zulip.Event{Type: "message", Message: &zulip.Message{
		ID:          7,
		Type:        "stream",
		SenderEmail: "owner@example.com",
		TopicName:   "onibi-s1",
		Content:     "/approve " + id,
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("state = %s", got.State)
	}
	var form url.Values
	select {
	case form = <-posted:
	case <-time.After(time.Second):
		t.Fatal("missing Zulip reply")
	}
	if form.Get("to") != "onibi" || form.Get("topic") != "onibi-s1" || !strings.Contains(form.Get("content"), "Approval "+id+" approve.") {
		t.Fatalf("form = %#v", form)
	}
}
