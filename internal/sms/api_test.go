package sms

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPostsTwilioForm(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		gotBody = r.Form.Encode()
		_ = json.NewEncoder(w).Encode(MessageResponse{SID: "SM123", Status: "queued"})
	}))
	defer srv.Close()
	c := New("AC123", "tok", "+15550001", "")
	c.BaseURL = srv.URL
	resp, err := c.Send(t.Context(), Message{To: "+15550002", Body: "approve https://onibi.example/a"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.SID != "SM123" || resp.Status != "queued" {
		t.Fatalf("resp = %#v", resp)
	}
	if gotPath != "/2010-04-01/Accounts/AC123/Messages.json" {
		t.Fatalf("path = %s", gotPath)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("AC123:tok"))
	if gotAuth != wantAuth {
		t.Fatalf("auth = %s", gotAuth)
	}
	for _, want := range []string{"To=%2B15550002", "From=%2B15550001", "Body=approve+https%3A%2F%2Fonibi.example%2Fa"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("body = %s missing %s", gotBody, want)
		}
	}
}

func TestSendUsesMessagingServiceSID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("MessagingServiceSid"); got != "MG123" {
			t.Fatalf("MessagingServiceSid = %q", got)
		}
		if got := r.Form.Get("From"); got != "" {
			t.Fatalf("From = %q", got)
		}
		_ = json.NewEncoder(w).Encode(MessageResponse{SID: "SM123"})
	}))
	defer srv.Close()
	c := New("AC123", "tok", "", "MG123")
	c.BaseURL = srv.URL
	if _, err := c.Send(t.Context(), Message{To: "+15550002", Body: "body"}); err != nil {
		t.Fatal(err)
	}
}

func TestSendSurfacesTwilioError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 21608, "message": "unverified number"})
	}))
	defer srv.Close()
	c := New("AC123", "tok", "+15550001", "")
	c.BaseURL = srv.URL
	_, err := c.Send(t.Context(), Message{To: "+15550002", Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "21608") {
		t.Fatalf("err = %v", err)
	}
}

func TestSendValidatesRequiredFields(t *testing.T) {
	c := New("", "", "", "")
	if _, err := c.Send(t.Context(), Message{}); err == nil {
		t.Fatal("expected validation error")
	}
}
