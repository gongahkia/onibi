package pushover

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestEmergencySendIncludesRetryExpireAndReceipt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages.json" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("priority") != "2" || r.FormValue("retry") != "30" || r.FormValue("expire") != "120" {
			t.Fatalf("form = %#v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(MessageResponse{Status: 1, Receipt: "r1"})
	}))
	defer srv.Close()
	c := New("app", "user")
	c.BaseURL = srv.URL
	got, err := c.Send(t.Context(), MessageOptions{Title: "Approval", Message: "Approve a1?", Priority: 2, Retry: 30 * time.Second, Expire: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if got.Receipt != "r1" {
		t.Fatalf("response = %#v", got)
	}
}

func TestApprovalFlow(t *testing.T) {
	var sent url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages.json" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		sent = r.PostForm
		_ = json.NewEncoder(w).Encode(MessageResponse{Status: 1, Receipt: "r1"})
	}))
	defer srv.Close()
	c := New("app", "user")
	c.BaseURL = srv.URL
	resp, err := c.Send(t.Context(), MessageOptions{
		Title:    "Onibi approval",
		Message:  "Approve a1",
		Priority: 2,
		Retry:    30 * time.Second,
		Expire:   time.Hour,
		URL:      "https://onibi.local/pushover/deny/a1",
		URLTitle: "Deny",
		Callback: "https://onibi.local/pushover/ack/a1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Receipt != "r1" {
		t.Fatalf("resp = %#v", resp)
	}
	want := map[string]string{
		"priority":  "2",
		"retry":     "30",
		"expire":    "3600",
		"url":       "https://onibi.local/pushover/deny/a1",
		"url_title": "Deny",
		"callback":  "https://onibi.local/pushover/ack/a1",
	}
	for key, value := range want {
		if sent.Get(key) != value {
			t.Fatalf("%s = %q, want %q in %#v", key, sent.Get(key), value, sent)
		}
	}
}

func TestReceiptPollingStopsOnAcknowledged(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/receipts/r1.json" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		ack := 0
		if calls == 2 {
			ack = 1
		}
		_ = json.NewEncoder(w).Encode(Receipt{Status: 1, Acknowledged: ack, AcknowledgedAt: int64(calls)})
	}))
	defer srv.Close()
	c := New("app", "user")
	c.BaseURL = srv.URL
	got, err := c.PollReceipt(t.Context(), "r1", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Acknowledged != 1 || calls != 2 {
		t.Fatalf("receipt=%#v calls=%s", got, strconv.Itoa(calls))
	}
}
