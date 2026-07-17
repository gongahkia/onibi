package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecordingRoutesAreAbsent(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0]
	for _, path := range []string{"/recordings", "/recordings/s1.cast", "/sessions/s1/recording.cast"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", path, w.Code)
		}
	}
}
