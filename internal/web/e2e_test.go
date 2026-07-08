package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	e2ecrypto "github.com/gongahkia/onibi/internal/e2e"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/store"
)

func TestRelayKeyBindStoresCommitmentOnly(t *testing.T) {
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
	if err := keys.RegisterPair(context.Background(), db, "tok", key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := keys.BindSession(context.Background(), db, "tok", sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Fatal("relay key not bound")
	}
	commit, ok, err := db.KVGetString(context.Background(), relaySessionCommitPrefix+sessionID)
	if err != nil || !ok {
		t.Fatalf("commit ok=%v err=%v", ok, err)
	}
	if commit != envelope.Commitment(key) {
		t.Fatal("bad commitment")
	}
	if strings.Contains(commit, envelope.EncodeKey(key)) {
		t.Fatal("commitment leaked raw key")
	}
	got, ok := keys.KeyForSession(sessionID)
	if !ok || !bytes.Equal(got, key) {
		t.Fatal("volatile session key missing")
	}
	verifyToken, err := relayVerifyToken(key, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok, err := db.WebSessionKeyVerifier(context.Background(), sessionID)
	if err != nil || !ok {
		t.Fatalf("stored verifier ok=%v err=%v", ok, err)
	}
	if !bytes.Equal(stored, verifyToken) {
		t.Fatal("stored verifier mismatch")
	}
	if err := srv.verifyRelayAttach(context.Background(), sessionID, base64.RawURLEncoding.EncodeToString(verifyToken)); err != nil {
		t.Fatal(err)
	}
	if err := srv.verifyRelayAttach(context.Background(), sessionID, base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, envelope.KeyBytes))); err == nil {
		t.Fatal("bad relay verifier accepted")
	}
}

func TestHealthzReturnsE2EVerifierForOwnerSession(t *testing.T) {
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
	if err := keys.RegisterPair(context.Background(), db, "tok", key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keys.BindSession(context.Background(), db, "tok", sessionID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.handleHealthz(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var got struct {
		OK             bool   `json:"ok"`
		E2E            bool   `json:"e2e"`
		KeyVerifierHex string `json:"key_verifier_hex"`
	}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want, err := relayVerifyToken(key, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK || !got.E2E || got.KeyVerifierHex != hex.EncodeToString(want) {
		t.Fatalf("healthz = %#v", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w = httptest.NewRecorder()
	srv.handleHealthz(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("no-cookie status = %d body = %q", w.Code, w.Body.String())
	}
	got = struct {
		OK             bool   `json:"ok"`
		E2E            bool   `json:"e2e"`
		KeyVerifierHex string `json:"key_verifier_hex"`
	}{}
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.E2E || got.KeyVerifierHex != "" {
		t.Fatalf("no-cookie healthz = %#v", got)
	}
}

func TestRelayControlBodyRequiresEncryption(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keys := NewRelayKeys()
	key, _ := envelope.NewKey()
	if err := keys.RegisterPair(context.Background(), db, "tok", key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	called := false
	srv.scroll = func(_ context.Context, sessionID, direction string) error {
		called = sessionID == "s1" && direction == "page_up"
		return nil
	}
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keys.BindSession(context.Background(), db, "tok", sessionID); err != nil {
		t.Fatal(err)
	}
	sessionKey := e2ecrypto.DeriveSessionKey(key, []byte(sessionID))
	streamID, err := envelope.NewStreamID()
	if err != nil {
		t.Fatal(err)
	}
	body, err := envelope.SealRelayFrame(sessionKey, sessionID, streamID, "http:POST:/control", e2eDirC2S, 0, e2eTypeText, []byte(`{"session_id":"s1","action":"page_up"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("page_up")) {
		t.Fatal("encrypted body leaked control payload")
	}
	req := httptest.NewRequest(http.MethodPost, "/control", bytes.NewReader(body))
	req.AddCookie(rr.Result().Cookies()[0])
	req.Header.Set("Content-Type", e2eContentType)
	w := httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("decrypted control did not reach handler")
	}

	req = httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"s1","action":"page_up"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	w = httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plaintext relay status = %d", w.Code)
	}
}

func TestRelayControlResponseIsEncrypted(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keys := NewRelayKeys()
	key, _ := envelope.NewKey()
	if err := keys.RegisterPair(context.Background(), db, "tok", key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	srv.scroll = func(_ context.Context, _, _ string) error { return nil }
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keys.BindSession(context.Background(), db, "tok", sessionID); err != nil {
		t.Fatal(err)
	}
	sessionKey := e2ecrypto.DeriveSessionKey(key, []byte(sessionID))
	streamID, err := envelope.NewStreamID()
	if err != nil {
		t.Fatal(err)
	}
	body, err := envelope.SealRelayFrame(sessionKey, sessionID, streamID, "http:POST:/control", e2eDirC2S, 0, e2eTypeText, []byte(`{"session_id":"s1","action":"page_up"}`))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/control", bytes.NewReader(body))
	req.AddCookie(rr.Result().Cookies()[0])
	req.Header.Set("Content-Type", e2eContentType)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatalf("encrypted response leaked JSON: %s", w.Body.String())
	}
	frame, plain, err := envelope.OpenRelayFrame(sessionKey, sessionID, "http:POST:/control", e2eDirS2C, 0, w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if frame.StreamID != streamID || frame.Type != e2eTypeText || !bytes.Contains(plain, []byte(`"ok":true`)) {
		t.Fatalf("frame=%#v plain=%q", frame, plain)
	}
}

func TestE2EReplayHTTPWithinTTLAndAfterExpiry(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Unix(1_700_000_000, 0)
	oldNow := e2eNow
	e2eNow = func() time.Time { return now }
	t.Cleanup(func() { e2eNow = oldNow })
	keys := NewRelayKeys()
	key, _ := envelope.NewKey()
	if err := keys.RegisterPair(context.Background(), db, "tok", key, time.Minute); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, RelayKeys: keys, RequireE2E: true})
	srv.scroll = func(_ context.Context, _, _ string) error { return nil }
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := keys.BindSession(context.Background(), db, "tok", sessionID); err != nil {
		t.Fatal(err)
	}
	sessionKey := e2ecrypto.DeriveSessionKey(key, []byte(sessionID))
	streamID, err := envelope.NewStreamID()
	if err != nil {
		t.Fatal(err)
	}
	body, err := envelope.SealRelayFrame(sessionKey, sessionID, streamID, "http:POST:/control", e2eDirC2S, 0, e2eTypeText, []byte(`{"session_id":"s1","action":"page_up"}`))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{http.StatusOK, http.StatusConflict, http.StatusOK} {
		if i == 1 {
			now = now.Add(e2eHTTPReplayTTL - time.Second)
		}
		if i == 2 {
			now = now.Add(2 * time.Second)
		}
		req := httptest.NewRequest(http.MethodPost, "/control", bytes.NewReader(body))
		req.AddCookie(rr.Result().Cookies()[0])
		req.Header.Set("Content-Type", e2eContentType)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("request %d status = %d body = %q", i+1, w.Code, w.Body.String())
		}
	}
}

func TestE2EReplayCacheBounded(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	oldNow := e2eNow
	e2eNow = func() time.Time { return now }
	t.Cleanup(func() { e2eNow = oldNow })
	srv := &Server{}
	for i := 0; i < e2eHTTPReplayLimit+16; i++ {
		frame := envelope.RelayFrame{
			SessionID: "session",
			StreamID:  "stream-" + strconv.Itoa(i),
			Channel:   "http:POST:/control",
			Direction: e2eDirC2S,
			Seq:       0,
		}
		if !srv.acceptE2EHTTPReplay(frame) {
			t.Fatalf("unique frame %d rejected", i)
		}
		if len(srv.e2eHTTPReplay) > e2eHTTPReplayLimit {
			t.Fatalf("replay cache size = %d, want <= %d", len(srv.e2eHTTPReplay), e2eHTTPReplayLimit)
		}
	}
}

func TestWSEncryptHidesPayload(t *testing.T) {
	key, _ := envelope.NewKey()
	server := newSeqWSCodec(key, "owner-session", e2eInfoPTY, e2eDirC2S, e2eDirS2C)
	client, err := newSeqWSClientCodec(key, "owner-session", e2eInfoPTY, e2eDirS2C, e2eDirC2S)
	if err != nil {
		t.Fatal(err)
	}
	typ, sealed, err := wsEncrypt(client, websocket.MessageBinary, []byte("pty secret"))
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText || bytes.Contains(sealed, []byte("pty secret")) {
		t.Fatalf("sealed typ=%v payload=%s", typ, sealed)
	}
	openedType, opened, err := wsDecrypt(server, typ, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if openedType != websocket.MessageBinary || string(opened) != "pty secret" {
		t.Fatalf("opened typ=%v payload=%q", openedType, opened)
	}
}

func TestSequencedWSEncryptRejectsReplay(t *testing.T) {
	key, _ := envelope.NewKey()
	server := newSeqWSCodec(key, "owner-session", e2eInfoPTY, e2eDirC2S, e2eDirS2C)
	client, err := newSeqWSClientCodec(key, "owner-session", e2eInfoPTY, e2eDirS2C, e2eDirC2S)
	if err != nil {
		t.Fatal(err)
	}
	typ, sealed, err := client.encrypt(websocket.MessageBinary, []byte("pty secret"))
	if err != nil {
		t.Fatal(err)
	}
	openedType, opened, err := server.decrypt(typ, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if openedType != websocket.MessageBinary || string(opened) != "pty secret" {
		t.Fatalf("opened typ=%v payload=%q", openedType, opened)
	}
	if _, _, err := server.decrypt(typ, sealed); err == nil {
		t.Fatal("replayed frame decrypted")
	}
	typ, sealed, err = client.encrypt(websocket.MessageText, []byte("next"))
	if err != nil {
		t.Fatal(err)
	}
	openedType, opened, err = server.decrypt(typ, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if openedType != websocket.MessageText || string(opened) != "next" {
		t.Fatalf("next opened typ=%v payload=%q", openedType, opened)
	}
}
