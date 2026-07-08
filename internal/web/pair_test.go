package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/setup"
	"github.com/gongahkia/onibi/internal/store"
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Fatalf("location = %q", loc)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != OwnerCookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies = %#v", cookies)
	}
	if !strings.Contains(resp.Header.Get("Referrer-Policy"), "no-referrer") {
		t.Fatalf("referrer-policy = %q", resp.Header.Get("Referrer-Policy"))
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

func TestViewerPairTokenCreatesViewerSession(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	token, err := setup.NewViewerToken(context.Background(), srv.db, "s1", time.Hour, 2)
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "/s/s1") {
		t.Fatalf("body missing session redirect: %q", string(body))
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	session, ok, err := srv.db.WebSession(context.Background(), cookies[0].Value)
	if err != nil || !ok {
		t.Fatalf("web session ok=%v err=%v", ok, err)
	}
	if session.Role != store.PairRoleViewer {
		t.Fatalf("role = %q, want viewer", session.Role)
	}
	if session.ShareSessionID != "s1" || session.ShareExpiresAt.IsZero() {
		t.Fatalf("viewer share binding = %#v", session)
	}

	resp2, err := client.Get(ts.URL + "/pair/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d", resp2.StatusCode)
	}
	resp3, err := client.Get(ts.URL + "/pair/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("third status = %d", resp3.StatusCode)
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
	body := w.Body.String()
	if !strings.Contains(body, "local HTTPS certificate") || !strings.Contains(body, "hotspot") {
		t.Fatalf("body missing diagnostic: %q", body)
	}
}

func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
