package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionRecordingEndpointServesCast(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.cast")
	if err := os.WriteFile(path, []byte(`{"version":2}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.recordingPath = func(_ context.Context, id string) (string, bool, error) {
		if id != "s1" {
			return "", false, nil
		}
		return path, true, nil
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions/s1/recording.cast", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"version":2`) {
		t.Fatalf("body = %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/x-asciicast") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestRecordingsEndpointListsAndServesCast(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.cast")
	if err := os.WriteFile(path, []byte(`{"version":2,"timestamp":200,"title":"s1"}`+"\n"+`[1.5,"o","ok"]`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.recordingList = func(_ context.Context) ([]RecordingSummary, error) {
		return []RecordingSummary{{
			ID:              "s1",
			SessionID:       "s1",
			Name:            "s1.cast",
			DurationSeconds: 1.5,
			SizeBytes:       64,
		}}, nil
	}
	srv.recordingPath = func(_ context.Context, id string) (string, bool, error) {
		if id != "s1" {
			return "", false, nil
		}
		return path, true, nil
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/recordings", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %q", w.Code, w.Body.String())
	}
	var body recordingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Recordings) != 1 || body.Recordings[0].URL != "/recordings/s1.cast" {
		t.Fatalf("recordings = %#v", body.Recordings)
	}

	req = httptest.NewRequest(http.MethodGet, "/recordings/s1.cast", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cast status = %d body = %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatalf("cast body = %q", w.Body.String())
	}
}
