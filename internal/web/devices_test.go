package web

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func pairDevice(t *testing.T, s *Server, remember bool) (browser, *http.Cookie, Device) {
	t.Helper()
	link, err := s.FreshPairingURL()
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	v, _ := url.ParseQuery(u.Fragment)
	raw, _ := json.Marshal(map[string]any{"token": v.Get("token"), "name": "Gabriel’s phone", "remember": remember})
	response := request(s, browser{}, "POST", "/api/pair", raw)
	if response.Code != 200 {
		t.Fatal(response.Body)
	}
	var session browserSession
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	var cookie, deviceCookie *http.Cookie
	for _, c := range response.Result().Cookies() {
		if c.Name == s.cookieName {
			cookie = c
		}
		if c.Name == s.deviceCookieName() {
			deviceCookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	return browser{cookie: cookie, csrf: session.CSRF}, deviceCookie, session.Device
}
func resumeDevice(s *Server, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", s.origin+"/api/session", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestRememberedDeviceExpiryRevocationAndStorage(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	now := time.Now().Truncate(time.Second)
	s.now = func() time.Time { return now }
	b, c, d := pairDevice(t, s, true)
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Domain != "" || !c.Expires.Equal(now.Add(rememberedLifetime)) {
		t.Fatalf("unsafe remembered cookie: %#v", c)
	}
	var hash []byte
	var name string
	if err := s.db.SQL().QueryRow(`SELECT token_hash,name FROM web_devices WHERE id=?`, d.ID).Scan(&hash, &name); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(c.Value))
	if string(hash) != string(want[:]) || name != d.Name {
		t.Fatal("credential not hashed or name missing")
	}
	temporary, tc, _ := pairDevice(t, s, false)
	if tc != nil {
		t.Fatal("temporary browser received persistent cookie")
	}
	restarted, err := New(Options{Backend: backend, DB: s.db, PublicOrigin: testOrigin, Now: s.now})
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []browser{b, temporary} {
		if got := request(restarted, old, "GET", "/api/session", nil); got.Code != 401 {
			t.Fatal("old short session survived restart")
		}
	}
	response := resumeDevice(restarted, c)
	if response.Code != 200 {
		t.Fatal(response.Body)
	}
	var session browserSession
	_ = json.Unmarshal(response.Body.Bytes(), &session)
	if session.Device.ID != d.ID || session.CSRF == b.csrf || !session.Device.ExpiresAt.Equal(d.ExpiresAt) {
		t.Fatal("renewal changed grant or reused CSRF")
	}
	resumed := browser{cookie: response.Result().Cookies()[0], csrf: session.CSRF}
	// A remembered secret cannot authorize a POST by itself.
	r := httptest.NewRequest("POST", testOrigin+"/api/actions", strings.NewReader(string(inputRequest("remembered-no-post", "never"))))
	r.AddCookie(c)
	r.Header.Set("Origin", testOrigin)
	w := httptest.NewRecorder()
	restarted.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("remembered cookie authorized mutation")
	}
	now = now.Add(rememberedLifetime - time.Second)
	if got := resumeDevice(restarted, c); got.Code != 200 {
		t.Fatal("early expiry")
	}
	now = now.Add(time.Second)
	if got := resumeDevice(restarted, c); got.Code != 401 {
		t.Fatal("30-day grant silently extended")
	}
	// Revocation invalidates all active sessions and remembered renewal.
	now = now.Add(-rememberedLifetime)
	if err := restarted.RevokeDevice(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if got := resumeDevice(restarted, c); got.Code != 401 {
		t.Fatal("revoked credential renewed")
	}
	if got := request(restarted, resumed, "GET", "/api/targets", nil); got.Code != 401 {
		t.Fatal("revoked active session worked")
	}
	if len(backend.calls) != 0 {
		t.Fatal("auth tests reached native execution")
	}
}
func TestDeviceOriginAndPairingRotation(t *testing.T) {
	s, b := testServer(t, testOrigin)
	old := pairingToken(t, s)
	browser, c, d := pairDevice(t, s, true)
	raw, _ := json.Marshal(map[string]string{"token": old})
	if got := request(s, browser, "POST", "/api/pair", raw); got.Code != 401 {
		t.Fatal("old grant survived rotation")
	}
	if _, err := s.FreshPairingURL(); err != nil {
		t.Fatal(err)
	}
	if got := request(s, browser, "GET", "/api/session", nil); got.Code != 200 {
		t.Fatal("grant rotation revoked session")
	}
	other, err := New(Options{Backend: b, DB: s.db, PublicOrigin: "https://other.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resumeDevice(other, c); got.Code != 401 {
		t.Fatal("grant crossed origins")
	}
	if err := other.RevokeDevice(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	if got := resumeDevice(s, c); got.Code != 200 {
		t.Fatal("different origin revoked device")
	}
	got := request(s, browser, "GET", "/api/devices", nil)
	if got.Code != 200 || strings.Contains(got.Body.String(), c.Value) || strings.Contains(got.Body.String(), "token_hash") {
		t.Fatal("device listing exposed credential")
	}
	if got := request(s, browser, "POST", "/api/logout", []byte(`{}`)); got.Code != 200 {
		t.Fatal(got.Body)
	}
	if got := resumeDevice(s, c); got.Code != 401 {
		t.Fatal("logout retained remembered access")
	}
}
func TestRevocationRejectsQueuedAction(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	b, _, d := pairDevice(t, s, true)
	s.execution <- struct{}{}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(s, b, "POST", "/api/actions", inputRequest("revoked-queued-action", "never")) }()
	deadline := time.Now().Add(time.Second)
	for {
		var count int
		err := s.db.SQL().QueryRow(`SELECT count(*) FROM web_action_receipts`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("action not queued")
		}
		time.Sleep(time.Millisecond)
	}
	if err := s.RevokeDevice(context.Background(), d.ID); err != nil {
		t.Fatal(err)
	}
	<-s.execution
	response := <-done
	if !strings.Contains(response.Body.String(), `"state":"rejected"`) || len(backend.calls) != 0 {
		t.Fatalf("revoked queued action ran: %s", response.Body)
	}
}
func TestDeviceNameValidation(t *testing.T) {
	for _, name := range []string{strings.Repeat("a", 65), "bad\nname", string([]byte{0xff})} {
		if _, ok := deviceName(name); ok {
			t.Fatalf("accepted %q", name)
		}
	}
}
