package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
	"github.com/gongahkia/onibi/internal/pty"
)

func TestControlCommandRetryExecutesOnceAndReturnsStatus(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	writes := make(chan []byte, 2)
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": pty.NewVirtualHost(func(p []byte) (int, error) {
			writes <- append([]byte(nil), p...)
			return len(p), nil
		}, nil, nil)}
	}
	rr := httptest.NewRecorder()
	ownerID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0]
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"command_id":"control-retry","session_id":"s1","action":"interrupt"}`))
		req.AddCookie(cookie)
		addCSRF(req, ownerID)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
		}
		var response controlResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.CommandID != "control-retry" || response.State != fleet.CommandSucceeded {
			t.Fatalf("response=%#v err=%v", response, err)
		}
	}
	select {
	case got := <-writes:
		if !bytes.Equal(got, []byte{3}) {
			t.Fatalf("write=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing control write")
	}
	select {
	case got := <-writes:
		t.Fatalf("duplicate write=%q", got)
	case <-time.After(50 * time.Millisecond):
	}
	req := httptest.NewRequest(http.MethodGet, "/control/control-retry", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status endpoint=%d body=%q", w.Code, w.Body.String())
	}
	var status controlResponse
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || status.CommandID != "control-retry" || status.State != fleet.CommandSucceeded {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestControlCommandsCoverInputHandoverAndKill(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	writes := make(chan []byte, 2)
	kills := make(chan struct{}, 1)
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": pty.NewVirtualHost(func(p []byte) (int, error) {
			writes <- append([]byte(nil), p...)
			return len(p), nil
		}, func() error {
			kills <- struct{}{}
			return nil
		}, nil)}
	}
	handover := false
	srv.handover = func(_ context.Context, sessionID, target string) (string, error) {
		handover = sessionID == "s1" && target == "phone"
		return "ready", nil
	}
	for _, req := range []controlRequest{
		{CommandID: "control-input", SessionID: "s1", Action: "input", Input: "pwd"},
		{CommandID: "control-kill", SessionID: "s1", Action: "kill"},
		{CommandID: "control-handover", SessionID: "s1", Action: "handover", Target: "phone"},
	} {
		command, err := srv.submitControl(context.Background(), req)
		if err != nil || command.State != fleet.CommandSucceeded {
			t.Fatalf("request=%#v command=%#v err=%v", req, command, err)
		}
	}
	for _, want := range [][]byte{[]byte("pwd"), []byte{'\n'}} {
		select {
		case got := <-writes:
			if !bytes.Equal(got, want) {
				t.Fatalf("write=%q want=%q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("missing input write")
		}
	}
	select {
	case <-kills:
	case <-time.After(time.Second):
		t.Fatal("missing kill")
	}
	if !handover {
		t.Fatal("handover not called")
	}
}
