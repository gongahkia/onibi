package web

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gongahkia/onibi/internal/pty"
	"github.com/gongahkia/onibi/internal/store"
)

func TestCertGenerateOrLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cert1, err := GenerateOrLoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	cert2, err := GenerateOrLoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert1.Certificate) == 0 || len(cert2.Certificate) == 0 {
		t.Fatal("missing certificate DER")
	}
	if !bytes.Equal(cert1.Certificate[0], cert2.Certificate[0]) {
		t.Fatal("loaded certificate differs from generated certificate")
	}
	leaf, err := x509.ParseCertificate(cert1.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "onibi-local-ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	ca, ok := parseSingleCert(caPEM)
	if !ok {
		t.Fatal("failed to parse ca cert")
	}
	if !ca.IsCA {
		t.Fatal("ca cert is not a CA")
	}
	if leaf.IsCA {
		t.Fatal("server cert is a CA")
	}
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("server cert not signed by ca: %v", err)
	}
	if !leaf.NotAfter.After(time.Now().AddDate(0, 11, 0)) {
		t.Fatalf("unexpected NotAfter: %s", leaf.NotAfter)
	}
	if !containsString(leaf.DNSNames, "localhost") {
		t.Fatalf("DNS SANs = %#v", leaf.DNSNames)
	}
	if !hasIP(leaf, "127.0.0.1") || !hasIP(leaf, "::1") {
		t.Fatalf("IP SANs = %#v", leaf.IPAddresses)
	}
	for _, name := range []string{"onibi-local-ca.crt", "onibi-local-ca.key", "onibi-local-ca.mobileconfig", "server.crt", "server.key"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o", name, got)
		}
	}
}

func TestCertIncludesExplicitListenHost(t *testing.T) {
	cert, err := GenerateOrLoadCertForHosts(t.TempDir(), "10.147.20.4", "[fd00:147::4]")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"10.147.20.4", "fd00:147::4"} {
		if !hasIP(leaf, host) {
			t.Fatalf("certificate missing %s: %#v", host, leaf.IPAddresses)
		}
	}
}

func TestCertRegeneratesTruncatedServerCert(t *testing.T) {
	dir := t.TempDir()
	cert1, err := GenerateOrLoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	paths := LocalCertPaths(dir)
	if err := os.Truncate(paths.ServerCert, 100); err != nil {
		t.Fatal(err)
	}
	cert2, err := GenerateOrLoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert1.Certificate) == 0 || len(cert2.Certificate) == 0 {
		t.Fatal("missing certificate DER")
	}
	if bytes.Equal(cert1.Certificate[0], cert2.Certificate[0]) {
		t.Fatal("truncated server cert was not regenerated")
	}
	info, err := os.Stat(paths.ServerCert)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 100 {
		t.Fatalf("server cert was not rewritten, size=%d", info.Size())
	}
	leaf, err := x509.ParseCertificate(cert2.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.NotBefore.After(time.Now()) || !leaf.NotAfter.After(time.Now().AddDate(0, 11, 0)) {
		t.Fatalf("unexpected validity: %s..%s", leaf.NotBefore, leaf.NotAfter)
	}
}

func TestCertRetriesFailedServerReadback(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	old := validateServerReadbackFunc
	validateServerReadbackFunc = func(paths CertPaths, caCert *x509.Certificate, now time.Time) (tls.Certificate, error) {
		calls++
		if calls == 1 {
			_ = os.Truncate(paths.ServerCert, 100)
			return tls.Certificate{}, errors.New("forced readback failure")
		}
		return old(paths, caCert, now)
	}
	t.Cleanup(func() { validateServerReadbackFunc = old })
	cert, err := GenerateOrLoadCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("readback calls = %d, want 2", calls)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("missing certificate DER")
	}
	info, err := os.Stat(LocalCertPaths(dir).ServerCert)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 100 {
		t.Fatalf("server cert was not rewritten, size=%d", info.Size())
	}
}

func TestOwnerCookieAttributesAndAuth(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	c := cookies[0]
	if c.Name != OwnerCookieName || c.Value != sessionID || c.Path != "/" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.MaxAge <= 0 {
		t.Fatalf("cookie = %#v", c)
	}
	req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"missing","action":"interrupt"}`))
	req.AddCookie(c)
	addCSRF(req, sessionID)
	w := httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code == http.StatusUnauthorized {
		t.Fatal("valid owner cookie was rejected")
	}
}

func TestPWAStaticFilesRequireAuthAndServe(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0]
	for _, tc := range []struct {
		path string
		ct   string
	}{
		{"/favicon.svg", "image/svg+xml"},
		{"/manifest.webmanifest", "application/manifest+json"},
		{"/sw.js", "application/javascript"},
		{"/icons/onibi-192.png", "image/png"},
		{"/fonts/JetBrainsMono-Regular.woff2", "font/woff2"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %q", tc.path, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, tc.ct) {
			t.Fatalf("%s content-type = %q", tc.path, ct)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unauth manifest status = %d", w.Code)
	}
}

func TestSPASessionRouteServesRoot(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/s/s1", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<!doctype html>") {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestWSPTYRejectsMissingCookie(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ws/pty?token=missing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestWSPTYAcceptsCookieAndToken(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/pty?token=" + sessionID
	header := http.Header{}
	header.Add("Cookie", rr.Result().Cookies()[0].String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{ptySubprotocol},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if got := c.Subprotocol(); got != ptySubprotocol {
		t.Fatalf("subprotocol = %q", got)
	}
}

func TestWSEventsAcceptsCookieAndToken(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/events?token=" + sessionID
	header := http.Header{}
	header.Add("Cookie", rr.Result().Cookies()[0].String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{eventsSubprotocol},
		HTTPHeader:   header,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if got := c.Subprotocol(); got != eventsSubprotocol {
		t.Fatalf("subprotocol = %q", got)
	}
	if err := wsjson.Write(ctx, c, eventsAttachFrame{Type: "attach"}); err != nil {
		t.Fatal(err)
	}
	var hello map[string]any
	if err := wsjson.Read(ctx, c, &hello); err != nil {
		t.Fatal(err)
	}
	payload, _ := hello["payload"].(map[string]any)
	if hello["type"] != "server.hello" || hello["ts"] == "" || payload["endpoint"] != "events" || payload["session_id"] != sessionID {
		t.Fatalf("hello = %#v", hello)
	}
}

func TestSessionInfoReturnsSinglePTYHost(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	host := pty.NewVirtualHost(nil, nil, nil)
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"local-shell": host}
	}
	rr := httptest.NewRecorder()
	_, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/session-info", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.handleSessionInfo(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	got := map[string]string{}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["session_id"] != "local-shell" || got["ws_token"] == "" || got["role"] != "" {
		t.Fatalf("session-info = %#v", got)
	}
}

func TestShareRoutesAreAbsent(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/share", nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s /share status=%d", method, w.Code)
		}
	}
}

func TestSessionInfoReturnsSingleSessionIDWithoutHost(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionIDs = func() []string { return []string{"s1"} }
	rr := httptest.NewRecorder()
	_, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/session-info", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.handleSessionInfo(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	got := map[string]string{}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["session_id"] != "s1" || got["ws_token"] == "" || got["role"] != "" {
		t.Fatalf("session-info = %#v", got)
	}
}

func TestSessionInfoEventsTokenSkipsSessionSelection(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionIDs = func() []string { return []string{"s1", "s2"} }
	rr := httptest.NewRecorder()
	ownerSessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/session-info?events=1", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.handleSessionInfo(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	got := map[string]string{}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["ws_token"] != ownerSessionID || got["session_id"] != "" || got["role"] != "" {
		t.Fatalf("session-info = %#v", got)
	}
}

func TestSessionCostEndpointIsUnavailable(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions/s1/cost", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestSessionsEndpointReturnsResolverRows(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionList = func(context.Context) ([]SessionSummary, error) {
		return []SessionSummary{{
			ID:                    "s1",
			Agent:                 "claude",
			CWD:                   "/tmp/repo",
			StartedAt:             "2026-06-30T00:00:00Z",
			LastActivity:          "2026-06-30T00:01:00Z",
			PendingApprovalsCount: 2,
		}}, nil
	}
	rr := httptest.NewRecorder()
	_, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var got []SessionSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "s1" || got[0].PendingApprovalsCount != 2 {
		t.Fatalf("sessions = %#v", got)
	}
}

func TestSessionsStatusEndpointDerivesStates(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	now := time.Now().UTC()
	old := now.Add(-10 * time.Minute).Format(time.RFC3339Nano)
	recent := now.Add(-time.Second).Format(time.RFC3339Nano)
	srv.sessionList = func(context.Context) ([]SessionSummary, error) {
		return []SessionSummary{
			{ID: "await", Agent: "claude", LastActivity: old, PendingApprovalsCount: 1},
			{ID: "work", Agent: "codex", LastActivity: recent},
			{ID: "idle", Agent: "opencode", LastActivity: old},
			{ID: "recover", Agent: "pi", LastActivity: recent, PendingApprovalsCount: 1, RecoveryState: store.SessionRecoveryRecovering},
			{ID: "failed", Agent: "pi", LastActivity: recent, PendingApprovalsCount: 1, RecoveryState: store.SessionRecoveryFailed},
		}, nil
	}
	rr := httptest.NewRecorder()
	_, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions/status?include=remote", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var got SessionsStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	states := map[string]SessionState{}
	for _, row := range got.Sessions {
		states[row.ID] = row.State
	}
	if states["await"] != SessionStateAwaitingApproval || states["work"] != SessionStateWorking || states["idle"] != SessionStateIdle || states["recover"] != SessionStateRecovering || states["failed"] != SessionStateFailed {
		t.Fatalf("states = %#v", states)
	}
	if got.Counts[SessionStateAwaitingApproval] != 1 || got.Counts[SessionStateWorking] != 1 || got.Counts[SessionStateIdle] != 1 || got.Counts[SessionStateRecovering] != 1 || got.Counts[SessionStateFailed] != 1 {
		t.Fatalf("counts = %#v", got.Counts)
	}
}

func TestDeriveSessionStateIsConsistentForCertifiedAdapters(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-10 * time.Minute).Format(time.RFC3339Nano)
	recent := now.Add(-time.Second).Format(time.RFC3339Nano)
	cases := []struct {
		name string
		row  SessionSummary
		want SessionState
	}{
		{name: "idle", row: SessionSummary{LastActivity: old}, want: SessionStateIdle},
		{name: "working", row: SessionSummary{LastActivity: recent}, want: SessionStateWorking},
		{name: "awaiting approval", row: SessionSummary{LastActivity: old, PendingApprovalsCount: 1}, want: SessionStateAwaitingApproval},
		{name: "recovering overrides approval", row: SessionSummary{LastActivity: recent, PendingApprovalsCount: 1, RecoveryState: store.SessionRecoveryReconnecting}, want: SessionStateRecovering},
		{name: "failed overrides approval", row: SessionSummary{LastActivity: recent, PendingApprovalsCount: 1, RecoveryState: store.SessionRecoveryFailed}, want: SessionStateFailed},
	}
	for _, agent := range []string{"claude", "codex", "pi"} {
		for _, tc := range cases {
			t.Run(agent+"/"+tc.name, func(t *testing.T) {
				row := tc.row
				row.Agent = agent
				if got := deriveSessionState(row, now); got != tc.want {
					t.Fatalf("state = %q, want %q", got, tc.want)
				}
			})
		}
	}
}

func TestSnapshotsEndpointReturnsResolverRows(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.snapshots = func(context.Context) ([]Snapshot, error) {
		return []Snapshot{{
			Name:             "branch",
			SessionID:        "s1",
			CreatedAt:        "2026-06-30T00:00:00Z",
			CWD:              "/tmp/repo",
			TranscriptOffset: 42,
		}}, nil
	}
	rr := httptest.NewRecorder()
	_, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/snapshots", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var got SnapshotListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Snapshots) != 1 || got.Snapshots[0].Name != "branch" || got.Snapshots[0].TranscriptOffset != 42 {
		t.Fatalf("snapshots = %#v", got)
	}
}

func TestSnapshotRestoreAndForkCallResolvers(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	restoreCalled := false
	forkCalled := false
	srv.snapshotRestore = func(_ context.Context, name string) (SnapshotActionResult, error) {
		restoreCalled = true
		if name != "branch" {
			t.Fatalf("restore name = %q", name)
		}
		return SnapshotActionResult{SessionID: "restored", Message: "ok"}, nil
	}
	srv.snapshotFork = func(_ context.Context, req SnapshotForkRequest) (SnapshotActionResult, error) {
		forkCalled = true
		if req.Name != "branch" || req.Turn != 7 || req.NewPrompt != "continue here" {
			t.Fatalf("fork req = %#v", req)
		}
		return SnapshotActionResult{SessionID: "forked", Message: "ok"}, nil
	}
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0]
	restoreReq := httptest.NewRequest(http.MethodPost, "/snapshots/restore", strings.NewReader(`{"name":"branch"}`))
	restoreReq.AddCookie(cookie)
	addCSRF(restoreReq, sessionID)
	restoreW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(restoreW, restoreReq)
	if restoreW.Code != http.StatusOK {
		t.Fatalf("restore status = %d body = %q", restoreW.Code, restoreW.Body.String())
	}
	var restore SnapshotActionResult
	if err := json.Unmarshal(restoreW.Body.Bytes(), &restore); err != nil {
		t.Fatal(err)
	}
	if restore.SessionID != "restored" || !restoreCalled {
		t.Fatalf("restore = %#v called=%v", restore, restoreCalled)
	}
	forkReq := httptest.NewRequest(http.MethodPost, "/snapshots/fork", strings.NewReader(`{"name":"branch","turn":7,"new_prompt":"continue here"}`))
	forkReq.AddCookie(cookie)
	addCSRF(forkReq, sessionID)
	forkW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(forkW, forkReq)
	if forkW.Code != http.StatusOK {
		t.Fatalf("fork status = %d body = %q", forkW.Code, forkW.Body.String())
	}
	var fork SnapshotActionResult
	if err := json.Unmarshal(forkW.Body.Bytes(), &fork); err != nil {
		t.Fatal(err)
	}
	if fork.SessionID != "forked" || !forkCalled {
		t.Fatalf("fork = %#v called=%v", fork, forkCalled)
	}
}

func TestControlInterruptUsesHostResolver(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	writes := make(chan []byte, 1)
	host := pty.NewVirtualHost(func(p []byte) (int, error) {
		writes <- append([]byte(nil), p...)
		return len(p), nil
	}, nil, nil)
	host.Cmd = &exec.Cmd{Process: &os.Process{Pid: -1}}
	srv.ptyHosts = func() map[string]*pty.Host {
		return map[string]*pty.Host{"s1": host}
	}
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"s1","action":"interrupt"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	addCSRF(req, sessionID)
	w := httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	select {
	case got := <-writes:
		if !bytes.Equal(got, []byte{3}) {
			t.Fatalf("write = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("host did not receive interrupt")
	}
}

func TestControlPageUpUsesScrollResolver(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	called := false
	srv.scroll = func(_ context.Context, sessionID, direction string) error {
		called = true
		if sessionID != "s1" || direction != "page_up" {
			t.Fatalf("scroll args = %q %q", sessionID, direction)
		}
		return nil
	}
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"s1","action":"page_up"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	addCSRF(req, sessionID)
	w := httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("scroll resolver was not called")
	}
}

func TestControlActionMapping(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   string
	}{
		{"interrupt", "signal:interrupt"},
		{"kill", "signal:kill"},
		{"page_up", "scroll:page_up"},
		{"page_down", "scroll:page_down"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			srv, cleanup := testServer(t)
			defer cleanup()
			got := make(chan string, 1)
			host := pty.NewVirtualHost(func(p []byte) (int, error) {
				if bytes.Equal(p, []byte{3}) {
					got <- "signal:interrupt"
				}
				return len(p), nil
			}, func() error {
				got <- "signal:kill"
				return nil
			}, nil)
			srv.ptyHosts = func() map[string]*pty.Host {
				return map[string]*pty.Host{"s1": host}
			}
			srv.scroll = func(_ context.Context, sessionID, direction string) error {
				if sessionID != "s1" {
					t.Fatalf("session id = %q", sessionID)
				}
				got <- "scroll:" + direction
				return nil
			}
			rr := httptest.NewRecorder()
			sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"s1","action":"`+tc.action+`"}`))
			req.AddCookie(rr.Result().Cookies()[0])
			addCSRF(req, sessionID)
			w := httptest.NewRecorder()
			srv.handleControl(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
			}
			select {
			case event := <-got:
				if event != tc.want {
					t.Fatalf("event = %q want %q", event, tc.want)
				}
			case <-time.After(time.Second):
				t.Fatal("control action not dispatched")
			}
		})
	}
}

func TestControlErrorsAreJSON(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		setup  func(*Server)
		status int
		msg    string
	}{
		{
			name:   "scroll unavailable",
			body:   `{"session_id":"s1","action":"page_up"}`,
			status: http.StatusNotImplemented,
			msg:    "scroll unavailable",
		},
		{
			name: "scroll failed",
			body: `{"session_id":"s1","action":"page_down"}`,
			setup: func(s *Server) {
				s.scroll = func(context.Context, string, string) error {
					return errors.New("scroll failed")
				}
			},
			status: http.StatusBadRequest,
			msg:    "scroll failed",
		},
		{
			name:   "session not found",
			body:   `{"session_id":"missing","action":"interrupt"}`,
			status: http.StatusNotFound,
			msg:    "session not found",
		},
		{
			name: "bad action",
			body: `{"session_id":"s1","action":"bad"}`,
			setup: func(s *Server) {
				s.ptyHosts = func() map[string]*pty.Host {
					return map[string]*pty.Host{"s1": pty.NewVirtualHost(nil, nil, nil)}
				}
			},
			status: http.StatusBadRequest,
			msg:    "bad action",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, cleanup := testServer(t)
			defer cleanup()
			if tc.setup != nil {
				tc.setup(srv)
			}
			rr := httptest.NewRecorder()
			sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(tc.body))
			req.AddCookie(rr.Result().Cookies()[0])
			addCSRF(req, sessionID)
			w := httptest.NewRecorder()
			srv.handleControl(w, req)
			if w.Code != tc.status {
				t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
			}
			if ct := w.Result().Header.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content-type = %q", ct)
			}
			var got struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Message != tc.msg {
				t.Fatalf("message = %q", got.Message)
			}
		})
	}
}

func TestHandoverCallsResolver(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	called := false
	srv.handover = func(_ context.Context, sessionID, target string) (string, error) {
		called = true
		if sessionID != "s1" || target != "mac" {
			t.Fatalf("handover args = %q %q", sessionID, target)
		}
		return "opened", nil
	}
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/handover", strings.NewReader(`{"session_id":"s1","target":"mac"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	addCSRF(req, sessionID)
	w := httptest.NewRecorder()
	srv.handleHandover(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("handover resolver was not called")
	}
}

func TestHandoverRejectsMissingCookie(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.handover = func(context.Context, string, string) (string, error) {
		t.Fatal("handover resolver should not be called")
		return "", nil
	}
	req := httptest.NewRequest(http.MethodPost, "/handover", strings.NewReader(`{"session_id":"s1","target":"mac"}`))
	w := httptest.NewRecorder()
	srv.handleHandover(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func testServer(t *testing.T) (*Server, func()) {
	t.Helper()
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db})
	return srv, func() { _ = db.Close() }
}

func containsString(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}

func hasIP(cert *x509.Certificate, want string) bool {
	for _, ip := range cert.IPAddresses {
		if ip.String() == want {
			return true
		}
	}
	return false
}
