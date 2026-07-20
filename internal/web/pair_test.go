package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	e2ecrypto "github.com/gongahkia/onibi/internal/e2e"
	"github.com/gongahkia/onibi/internal/envelope"
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

func TestE2EPairConfirmRejectsBadVerifierBeforeClaim(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keys := NewRelayKeys()
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	token, err := setup.NewToken(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.RegisterPair(ctx, db, token, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	handler := srv.Handler()

	getReq := httptest.NewRequest(http.MethodGet, "/pair/"+token, nil)
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("pair page status = %d body = %q", getW.Code, getW.Body.String())
	}
	if len(getW.Result().Cookies()) != 0 {
		t.Fatalf("pair page set cookies = %#v", getW.Result().Cookies())
	}
	body := getW.Body.String()
	for _, want := range []string{"location.hash", "history.replaceState", "http:POST:/pair/confirm", "fetch('/pair/confirm'"} {
		if !strings.Contains(body, want) {
			t.Fatalf("pair page missing %q: %q", want, body)
		}
	}

	badVerifier := bytes.Repeat([]byte{0x7}, envelope.KeyBytes)
	badFrame := sealPairConfirm(t, key, token, badVerifier)
	badReq := httptest.NewRequest(http.MethodPost, "/pair/confirm", bytes.NewReader(badFrame.body))
	badReq.Header.Set("Content-Type", e2eContentType)
	badW := httptest.NewRecorder()
	handler.ServeHTTP(badW, badReq)
	if badW.Code != http.StatusUnauthorized {
		t.Fatalf("bad verifier status = %d body = %q", badW.Code, badW.Body.String())
	}

	wantVerifier, err := relayPairVerifier(key, token)
	if err != nil {
		t.Fatal(err)
	}
	okFrame := sealPairConfirm(t, key, token, wantVerifier)
	okReq := httptest.NewRequest(http.MethodPost, "/pair/confirm", bytes.NewReader(okFrame.body))
	okReq.Header.Set("Content-Type", e2eContentType)
	okW := httptest.NewRecorder()
	handler.ServeHTTP(okW, okReq)
	if okW.Code != http.StatusOK {
		t.Fatalf("good verifier status = %d body = %q", okW.Code, okW.Body.String())
	}
	if ct := okW.Header().Get("Content-Type"); ct != e2eContentType {
		t.Fatalf("content-type = %q", ct)
	}
	if strings.Contains(okW.Body.String(), "session_id") {
		t.Fatalf("encrypted response leaked JSON: %q", okW.Body.String())
	}
	responseFrame, plain, err := envelope.OpenRelayFrame(e2ecrypto.DerivePairConfirmKey(key, []byte(token)), token, e2eInfoPairConfirm, e2eDirS2C, 0, okW.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if responseFrame.StreamID != okFrame.streamID || responseFrame.Type != e2eTypeText {
		t.Fatalf("response frame = %#v", responseFrame)
	}
	var got pairConfirmResponse
	if err := json.Unmarshal(plain, &got); err != nil {
		t.Fatal(err)
	}
	cookies := okW.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != OwnerCookieName || got.SessionID != cookies[0].Value {
		t.Fatalf("response=%#v cookies=%#v", got, cookies)
	}
	gotKey, ok := keys.KeyForSession(got.SessionID)
	if !ok || !bytes.Equal(gotKey, key) {
		t.Fatal("session key not bound")
	}
	sessionVerifier, ok, err := db.WebSessionKeyVerifier(ctx, got.SessionID)
	if err != nil || !ok {
		t.Fatalf("session verifier ok=%v err=%v", ok, err)
	}
	wantSessionVerifier, err := relayVerifyToken(key, got.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sessionVerifier, wantSessionVerifier) {
		t.Fatal("session verifier mismatch")
	}

	reuseFrame := sealPairConfirm(t, key, token, wantVerifier)
	reuseReq := httptest.NewRequest(http.MethodPost, "/pair/confirm", bytes.NewReader(reuseFrame.body))
	reuseReq.Header.Set("Content-Type", e2eContentType)
	reuseW := httptest.NewRecorder()
	handler.ServeHTTP(reuseW, reuseReq)
	if reuseW.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d body = %q", reuseW.Code, reuseW.Body.String())
	}
}

func TestE2EPairConfirmRejectsPlaintext(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keys := NewRelayKeys()
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	token, err := setup.NewToken(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.RegisterPair(ctx, db, token, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	req := httptest.NewRequest(http.MethodPost, "/pair/confirm", strings.NewReader(`{"verifier":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
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

type sealedPairConfirm struct {
	body     []byte
	streamID string
}

func sealPairConfirm(t *testing.T, key []byte, token string, verifier []byte) sealedPairConfirm {
	t.Helper()
	streamID, err := envelope.NewStreamID()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(pairConfirmRequest{Verifier: base64.RawURLEncoding.EncodeToString(verifier)})
	if err != nil {
		t.Fatal(err)
	}
	body, err := envelope.SealRelayFrame(e2ecrypto.DerivePairConfirmKey(key, []byte(token)), token, streamID, e2eInfoPairConfirm, e2eDirC2S, 0, e2eTypeText, plain)
	if err != nil {
		t.Fatal(err)
	}
	return sealedPairConfirm{body: body, streamID: streamID}
}
