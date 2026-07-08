package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func TestShareCreateListAndRevokeViewer(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionIDs = func() []string { return []string{"s1"} }
	rr := httptest.NewRecorder()
	ownerID, err := srv.CreateOwnerSession(context.Background(), rr, "owner")
	if err != nil {
		t.Fatal(err)
	}
	ownerCookie := rr.Result().Cookies()[0]

	createReq := httptest.NewRequest(http.MethodPost, "/share", strings.NewReader(`{"session_id":"s1","ttl":"30m","max_viewers":2}`))
	createReq.Host = "phone.local:9443"
	createReq.AddCookie(ownerCookie)
	addCSRF(createReq, ownerID)
	createW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %q", createW.Code, createW.Body.String())
	}
	var created shareCreateResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.SessionID != "s1" || created.Role != store.PairRoleViewer || created.MaxViewers != 2 || created.QRPNGData == "" {
		t.Fatalf("created = %#v", created)
	}
	parsed, err := url.Parse(created.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "phone.local:9443" || parsed.Fragment != "/s/s1" {
		t.Fatalf("url = %s", created.URL)
	}
	token := strings.TrimPrefix(parsed.Path, "/pair/")
	if token == parsed.Path || token == "" {
		t.Fatalf("missing pair token in %s", created.URL)
	}

	pairReq := httptest.NewRequest(http.MethodGet, "/pair/"+token, nil)
	pairReq.Header.Set("User-Agent", "ViewerAgent/1.0")
	pairW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(pairW, pairReq)
	if pairW.Code != http.StatusOK {
		t.Fatalf("pair status = %d body = %q", pairW.Code, pairW.Body.String())
	}
	viewerCookie := pairW.Result().Cookies()[0]
	viewer, ok, err := srv.db.WebSession(context.Background(), viewerCookie.Value)
	if err != nil || !ok {
		t.Fatalf("viewer ok=%v err=%v", ok, err)
	}
	if viewer.Role != store.PairRoleViewer || viewer.ShareSessionID != "s1" || viewer.ShareExpiresAt.IsZero() {
		t.Fatalf("viewer = %#v", viewer)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/share?session_id=s1", nil)
	listReq.AddCookie(ownerCookie)
	listW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %q", listW.Code, listW.Body.String())
	}
	var listed shareListResponse
	if err := json.Unmarshal(listW.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Viewers) != 1 || listed.Viewers[0].ID != viewerCookie.Value || listed.Viewers[0].ExpiresAt == "" {
		t.Fatalf("listed = %#v", listed)
	}

	revokeReq := httptest.NewRequest(http.MethodPost, "/share/revoke", strings.NewReader(`{"session_id":"s1","viewer_id":"`+viewerCookie.Value+`"}`))
	revokeReq.AddCookie(ownerCookie)
	addCSRF(revokeReq, ownerID)
	revokeW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(revokeW, revokeReq)
	if revokeW.Code != http.StatusOK {
		t.Fatalf("revoke status = %d body = %q", revokeW.Code, revokeW.Body.String())
	}
	valid, err := srv.db.WebSessionValid(context.Background(), viewerCookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("viewer session still valid after revoke")
	}
}

func TestShareRejectsViewerOwnerEndpoint(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionIDs = func() []string { return []string{"s1"} }
	rr := httptest.NewRecorder()
	if _, err := srv.CreateViewerSession(context.Background(), rr, "viewer", "s1", futureShareExpiry()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/share", strings.NewReader(`{"session_id":"s1","ttl":"5m","max_viewers":1}`))
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func futureShareExpiry() time.Time {
	return time.Now().Add(time.Hour)
}
