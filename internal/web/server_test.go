package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/store"
)

const testOrigin = "https://onibi.example.test"

type fakeBackend struct {
	mu        sync.Mutex
	targets   []ghostty.Target
	calls     []ghostty.Action
	listCalls int
	start     chan struct{}
	release   chan struct{}
	execErr   error
	snapErr   error
	snapText  string
}

func (f *fakeBackend) Targets(context.Context) ([]ghostty.Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	return append([]ghostty.Target(nil), f.targets...), nil
}

func (f *fakeBackend) Snapshot(_ context.Context, id string) (ghostty.Snapshot, error) {
	return ghostty.Snapshot{TargetID: id, Text: f.snapText, CapturedAt: time.Now()}, f.snapErr
}

func (f *fakeBackend) Execute(ctx context.Context, action ghostty.Action) (ghostty.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, action)
	f.mu.Unlock()
	if f.start != nil {
		close(f.start)
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return ghostty.Result{}, ctx.Err()
		}
	}
	return ghostty.Result{TargetID: action.TargetID, Message: "Input sent."}, f.execErr
}

func testServer(t *testing.T, origin string) (*Server, *fakeBackend) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "web.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backend := &fakeBackend{targets: []ghostty.Target{{ID: "surface-1", WindowID: "window-1", TabID: "tab-1"}}}
	s, err := New(Options{Backend: backend, DB: db, PublicOrigin: origin, Assets: fstest.MapFS{"index.html": {Data: []byte("Onibi")}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, backend
}

type browser struct {
	cookie *http.Cookie
	csrf   string
}

func pairingToken(t *testing.T, s *Server) string {
	t.Helper()
	link, err := url.Parse(s.PairingURL())
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(link.Fragment)
	if err != nil || values.Get("token") == "" {
		t.Fatalf("invalid pairing URL: %v", err)
	}
	return values.Get("token")
}

func pairBrowser(t *testing.T, s *Server) browser {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"token": pairingToken(t, s)})
	response := request(s, browser{}, http.MethodPost, "/api/pair", raw)
	if response.Code != http.StatusOK {
		t.Fatalf("pair: %d %s", response.Code, response.Body)
	}
	var session browserSession
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || session.CSRF == "" {
		t.Fatalf("missing session credentials: cookies=%d", len(cookies))
	}
	return browser{cookie: cookies[0], csrf: session.CSRF}
}

func request(s *Server, b browser, method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, s.origin+path, bytes.NewReader(body))
	r.Header.Set("Origin", s.origin)
	r.Header.Set("Content-Type", "application/json")
	if b.cookie != nil {
		r.AddCookie(b.cookie)
		r.Header.Set("X-Onibi-CSRF", b.csrf)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func inputRequest(id, text string) []byte {
	raw, _ := json.Marshal(actionRequest{ID: id, Action: ghostty.Action{Kind: "input", TargetID: "surface-1", Text: text, Enter: true}})
	return raw
}

func TestWebAuthenticationAndCookieSecurity(t *testing.T) {
	for _, origin := range []string{testOrigin, "http://127.0.0.1:8787"} {
		t.Run(origin, func(t *testing.T) {
			s, backend := testServer(t, origin)
			for _, path := range []string{"/api/session", "/api/targets", "/api/snapshot?targetId=surface-1", "/api/actions/operation-000001"} {
				if got := request(s, browser{}, http.MethodGet, path, nil); got.Code != http.StatusUnauthorized {
					t.Fatalf("unauthenticated %s: %d", path, got.Code)
				}
			}
			if got := request(s, browser{}, http.MethodPost, "/api/actions", inputRequest("operation-000001", "test")); got.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated write: %d", got.Code)
			}
			if backend.listCalls != 0 || len(backend.calls) != 0 {
				t.Fatal("unauthenticated request reached native backend")
			}
			b := pairBrowser(t, s)
			secure := strings.HasPrefix(origin, "https:")
			if b.cookie.Secure != secure || !b.cookie.HttpOnly || b.cookie.SameSite != http.SameSiteStrictMode || b.cookie.Path != "/" || b.cookie.Domain != "" {
				t.Fatalf("unsafe cookie attributes: %#v", b.cookie)
			}
			if secure && !strings.HasPrefix(b.cookie.Name, "__Host-") {
				t.Fatal("HTTPS cookie lacks __Host- prefix")
			}
			got := request(s, b, http.MethodGet, "/api/session", nil)
			if got.Code != http.StatusOK || got.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("session: %d %s", got.Code, got.Body)
			}
			if got := request(s, b, http.MethodPost, "/api/logout", []byte(`{}`)); got.Code != http.StatusOK {
				t.Fatalf("logout: %d", got.Code)
			}
			if got := request(s, b, http.MethodGet, "/api/session", nil); got.Code != http.StatusUnauthorized {
				t.Fatal("logout did not revoke session")
			}
		})
	}
}

func TestWebPairingIsSingleUseAndExpires(t *testing.T) {
	s, _ := testServer(t, testOrigin)
	token := pairingToken(t, s)
	if s.PairingURL() != "" {
		t.Fatal("raw pairing secret remained retrievable")
	}
	raw, _ := json.Marshal(map[string]string{"token": token})
	results := make(chan int, 8)
	var wg sync.WaitGroup
	for range cap(results) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- request(s, browser{}, http.MethodPost, "/api/pair", raw).Code
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == http.StatusOK {
			success++
		} else if status != http.StatusUnauthorized {
			t.Fatalf("unexpected pairing status %d", status)
		}
	}
	if success != 1 {
		t.Fatalf("pairing succeeded %d times", success)
	}
	expired, _ := testServer(t, testOrigin)
	raw, _ = json.Marshal(map[string]string{"token": pairingToken(t, expired)})
	expired.now = func() time.Time { return time.Now().Add(pairingLifetime + time.Minute) }
	if got := request(expired, browser{}, http.MethodPost, "/api/pair", raw); got.Code != http.StatusUnauthorized {
		t.Fatalf("expired pairing: %d", got.Code)
	}
}

func TestWebRejectsHostOriginCSRFAndMalformedBodies(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	for _, test := range []struct {
		name string
		edit func(*http.Request)
		body []byte
		want int
	}{
		{"host", func(r *http.Request) { r.Host = "attacker.example" }, nil, 403},
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "https://onibi.example.test.attacker.invalid") }, nil, 403},
		{"null_origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, nil, 403},
		{"missing_origin", func(r *http.Request) { r.Header.Del("Origin") }, nil, 403},
		{"cross_site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, nil, 403},
		{"csrf", func(r *http.Request) { r.Header.Set("X-Onibi-CSRF", "wrong") }, nil, 403},
		{"content_type", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, nil, 415},
		{"unknown_field", func(*http.Request) {}, []byte(`{"id":"operation-000001","surprise":true}`), 400},
		{"trailing_object", func(*http.Request) {}, append(inputRequest("operation-000001", "test"), []byte(`{}`)...), 400},
		{"body_size", func(*http.Request) {}, []byte(`{"id":"` + strings.Repeat("x", maxBodyBytes+1) + `"}`), 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := test.body
			if body == nil {
				body = inputRequest("operation-000001", "test")
			}
			r := httptest.NewRequest(http.MethodPost, testOrigin+"/api/actions", bytes.NewReader(body))
			r.AddCookie(b.cookie)
			r.Header.Set("Origin", testOrigin)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Onibi-CSRF", b.csrf)
			test.edit(r)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status=%d want=%d: %s", w.Code, test.want, w.Body)
			}
		})
	}
	if backend.listCalls != 0 || len(backend.calls) != 0 {
		t.Fatal("rejected request reached native backend")
	}
}

func TestWebOriginsAndTrustedProxyHosts(t *testing.T) {
	for _, origin := range []string{"http://0.0.0.0:8787", "http://192.168.1.2", "https://user:pass@example.test", "https://example.test/path", "https://example.test?token=x", "https://example.test#token=x", "https://example.test?", "https://example.test:99999", "https://example.test:", "file:///tmp/onibi"} {
		if _, err := parseOrigin(origin); err == nil {
			t.Fatalf("accepted origin %q", origin)
		}
	}
	canonical, err := parseOrigin("https://ONIBI.example.test:443/")
	if err != nil || canonical.String() != testOrigin {
		t.Fatalf("origin normalization: %v %v", canonical, err)
	}
	s, _ := testServer(t, testOrigin)
	s.proxyHosts["127.0.0.1:8787"] = true
	for _, test := range []struct {
		host, peer string
		want       int
	}{
		{"onibi.example.test", "127.0.0.1:5000", 200},
		{"127.0.0.1:8787", "127.0.0.1:5000", 200},
		{"127.0.0.1:8787", "192.168.1.3:5000", 403},
		{"127.0.0.1:9999", "127.0.0.1:5000", 403},
	} {
		r := httptest.NewRequest(http.MethodGet, testOrigin+"/", nil)
		r.Host, r.RemoteAddr = test.host, test.peer
		r.Header.Set("X-Forwarded-Host", "onibi.example.test")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("host=%s peer=%s status=%d", test.host, test.peer, w.Code)
		}
	}
}

func TestWebCommandsClaimBeforeExecutionAndReplayOnlySamePayload(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	backend.start, backend.release = make(chan struct{}), make(chan struct{})
	raw := inputRequest("operation-000001", "private shell input")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(s, b, http.MethodPost, "/api/actions", raw) }()
	select {
	case <-backend.start:
	case <-time.After(time.Second):
		t.Fatal("backend did not start")
	}
	got := request(s, b, http.MethodPost, "/api/actions", raw)
	if receipt := decodeReceipt(t, got); receipt.State != "pending" {
		t.Fatalf("concurrent retry=%#v", receipt)
	}
	if got := request(s, b, http.MethodPost, "/api/actions", inputRequest("operation-000001", "different input")); got.Code != http.StatusConflict {
		t.Fatalf("conflicting reuse=%d %s", got.Code, got.Body)
	}
	close(backend.release)
	if receipt := decodeReceipt(t, <-done); receipt.State != "completed" {
		t.Fatalf("original receipt=%#v", receipt)
	}
	if receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", raw)); receipt.State != "completed" {
		t.Fatalf("retry receipt=%#v", receipt)
	}
	if receipt := decodeReceipt(t, request(s, b, http.MethodGet, "/api/actions/operation-000001", nil)); receipt.State != "completed" {
		t.Fatalf("status receipt=%#v", receipt)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.calls) != 1 {
		t.Fatalf("executed %d times", len(backend.calls))
	}
	var storedHash, result, storedError string
	if err := s.db.SQL().QueryRow(`SELECT payload_hash,COALESCE(result_json,''),error FROM web_action_receipts`).Scan(&storedHash, &result, &storedError); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedHash+result+storedError, "private shell input") {
		t.Fatal("raw input leaked into durable receipt")
	}
}

func TestWebRestartRevokesSessionAndNeverReplaysInterruptedCommand(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	raw := inputRequest("operation-000001", "test")
	if receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", raw)); receipt.State != "completed" {
		t.Fatal("command did not complete")
	}
	if _, err := s.db.SQL().Exec(`UPDATE web_action_receipts SET state='pending',result_json=NULL`); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(Options{Backend: backend, DB: s.db, PublicOrigin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	if got := request(restarted, b, http.MethodGet, "/api/session", nil); got.Code != http.StatusUnauthorized {
		t.Fatal("restart did not revoke old browser cookie")
	}
	fresh := pairBrowser(t, restarted)
	if receipt := decodeReceipt(t, request(restarted, fresh, http.MethodPost, "/api/actions", raw)); receipt.State != "uncertain" {
		t.Fatalf("recovered receipt=%#v", receipt)
	}
	if len(backend.calls) != 1 {
		t.Fatal("restart replayed an interrupted command")
	}
}

func TestWebExpiredSessionAndStaleTargetsCannotExecute(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	backend.targets = nil
	if receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", inputRequest("operation-000001", "test"))); receipt.State != "rejected" {
		t.Fatalf("stale target receipt=%#v", receipt)
	}
	if len(backend.calls) != 0 {
		t.Fatal("stale target reached Execute")
	}
	s.now = func() time.Time { return time.Now().Add(sessionLifetime + time.Minute) }
	if got := request(s, b, http.MethodPost, "/api/actions", inputRequest("operation-000002", "test")); got.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", got.Code)
	}
}

func TestWebExecutionFailuresRemainUncertainWithoutInternalDetails(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	backend.execErr = errors.New("internal script with secret-user-input failed")
	raw := inputRequest("operation-000001", "test")
	for range 2 {
		receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", raw))
		if receipt.State != "uncertain" || strings.Contains(receipt.Error, "secret-user-input") {
			t.Fatalf("failure receipt=%#v", receipt)
		}
	}
	if len(backend.calls) != 1 {
		t.Fatal("uncertain command was retried")
	}
}

func TestWebAbandonedPendingReceiptBecomesUncertainWithoutReplay(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	raw := inputRequest("operation-000001", "test")
	if receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", raw)); receipt.State != "completed" {
		t.Fatal("command did not complete")
	}
	if _, err := s.db.SQL().Exec(`UPDATE web_action_receipts SET state='pending',result_json=NULL,created_at=?`, time.Now().Add(-time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if receipt := decodeReceipt(t, request(s, b, http.MethodGet, "/api/actions/operation-000001", nil)); receipt.State != "uncertain" {
		t.Fatalf("abandoned receipt=%#v", receipt)
	}
	var persisted string
	if err := s.db.SQL().QueryRow(`SELECT state FROM web_action_receipts`).Scan(&persisted); err != nil || persisted != "pending" {
		t.Fatalf("GET mutated persisted claim: state=%s err=%v", persisted, err)
	}
	if receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", raw)); receipt.State != "uncertain" || len(backend.calls) != 1 {
		t.Fatalf("abandoned claim was replayed: %#v calls=%d", receipt, len(backend.calls))
	}
}

func TestWebSnapshotFocusAndBoundedInertText(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	backend.snapErr = ghostty.ErrFocusRequired
	got := request(s, b, http.MethodGet, "/api/snapshot?targetId=surface-1", nil)
	if got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "focus_required") {
		t.Fatalf("focus error: %d %s", got.Code, got.Body)
	}
	backend.snapErr = nil
	backend.snapText = strings.Repeat("x", maxSnapshotBytes) + "<script>alert(1)</script>"
	got = request(s, b, http.MethodGet, "/api/snapshot?targetId=surface-1", nil)
	var snapshot ghostty.Snapshot
	if err := json.Unmarshal(got.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Text) > maxSnapshotBytes || strings.Contains(got.Body.String(), "<script>") {
		t.Fatal("snapshot is unbounded or contains unescaped HTML")
	}
	if got.Header().Get("Content-Type") != "application/json; charset=utf-8" || !strings.Contains(got.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("snapshot lacks content security headers")
	}
}

func TestWebTabActionsRequireExplicitNativeRelationship(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b := pairBrowser(t, s)
	for i, action := range []ghostty.Action{
		{Kind: "new_tab"},
		{Kind: "close_tab", TabID: "tab-1"},
		{Kind: "new_tab", WindowID: "missing"},
		{Kind: "close_tab", WindowID: "other-window", TabID: "tab-1"},
	} {
		raw, _ := json.Marshal(actionRequest{ID: fmt.Sprintf("tab-operation-%04d", i), Action: action})
		got := request(s, b, http.MethodPost, "/api/actions", raw)
		if i < 2 && got.Code != http.StatusBadRequest {
			t.Fatalf("missing target status=%d", got.Code)
		}
		if i >= 2 && decodeReceipt(t, got).State != "rejected" {
			t.Fatal("invalid native relationship was not rejected")
		}
	}
	if len(backend.calls) != 0 {
		t.Fatal("invalid tab request reached Execute")
	}
}

func decodeReceipt(t *testing.T, response *httptest.ResponseRecorder) Receipt {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("receipt status=%d body=%s", response.Code, response.Body)
	}
	var receipt Receipt
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestCockpitLifecycleAndControls(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	backend.targets[0].Focused = true
	b := pairBrowser(t, s)
	for i, action := range []ghostty.Action{
		{Kind: "new_window"},
		{Kind: "split", TargetID: "surface-1", Direction: "down"},
		{Kind: "close_pane", TargetID: "surface-1"},
		{Kind: "close_window", WindowID: "window-1"},
		{Kind: "rename_tab", TargetID: "surface-1", Name: "Agent review"},
		{Kind: "control", TargetID: "surface-1", Control: "scroll_up"},
	} {
		raw, _ := json.Marshal(actionRequest{ID: fmt.Sprintf("cockpit-action-%04d", i), Action: action})
		response := request(s, b, http.MethodPost, "/api/actions", raw)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"completed"`) {
			t.Fatalf("%s: %d %s", action.Kind, response.Code, response.Body)
		}
	}
	before := len(backend.calls)
	for i, action := range []ghostty.Action{
		{Kind: "split", TargetID: "missing", Direction: "down"},
		{Kind: "close_window", WindowID: "missing"},
		{Kind: "control", TargetID: "surface-1", Control: "quit"},
		{Kind: "rename_pane", TargetID: "surface-1", Name: "bad\nname"},
	} {
		raw, _ := json.Marshal(actionRequest{ID: fmt.Sprintf("cockpit-reject-%04d", i), Action: action})
		response := request(s, b, http.MethodPost, "/api/actions", raw)
		if response.Code == http.StatusOK && !strings.Contains(response.Body.String(), `"state":"rejected"`) {
			t.Fatalf("accepted %s: %s", action.Kind, response.Body)
		}
	}
	if len(backend.calls) != before {
		t.Fatal("invalid cockpit actions reached Ghostty")
	}
	backend.targets = nil
	raw, _ := json.Marshal(actionRequest{ID: "empty-app-new-window", Action: ghostty.Action{Kind: "new_window"}})
	response := request(s, b, http.MethodPost, "/api/actions", raw)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"completed"`) {
		t.Fatalf("cannot recover an empty Ghostty: %s", response.Body)
	}
}
