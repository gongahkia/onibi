package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/setup"
)

func TestPairTokenSuccessAndReuse(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	token, err := setup.NewToken(context.Background(), srv.db)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client := noRedirectClient()

	resp, err := client.Get(ts.URL + "/pair/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("location = %q", loc)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != OwnerCookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies = %#v", cookies)
	}

	rootReq, err := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	rootReq.AddCookie(cookies[0])
	rootResp, err := http.DefaultClient.Do(rootReq)
	if err != nil {
		t.Fatal(err)
	}
	defer rootResp.Body.Close()
	if rootResp.StatusCode != http.StatusOK {
		t.Fatalf("root status = %d", rootResp.StatusCode)
	}

	reuseReq, err := http.NewRequest(http.MethodGet, ts.URL+"/pair/"+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	reuseReq.AddCookie(cookies[0])
	reuseResp, err := client.Do(reuseReq)
	if err != nil {
		t.Fatal(err)
	}
	defer reuseResp.Body.Close()
	if reuseResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d", reuseResp.StatusCode)
	}
}

func TestPairTokenExpiredFails(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	if err := srv.db.PutPairingToken(context.Background(), "expired-token", -time.Minute); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := noRedirectClient().Get(ts.URL + "/pair/expired-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestRootWithoutOwnerCookieForbidden(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleRoot(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d", w.Code)
	}
}

func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
