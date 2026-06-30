package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/store"
)

func TestPushVAPIDKeysSurviveRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "onibi.db")
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dbPath, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	first, err := EnsureVAPIDKeys(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if first.PrivateKey == "" || first.PublicKey == "" {
		t.Fatalf("empty keys = %#v", first)
	}
	_ = db.Close()

	db, err = store.Open(dbPath, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	second, err := EnsureVAPIDKeys(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("keys changed after restart: %#v != %#v", second, first)
	}
}

func TestPushVAPIDPublicKeyEndpoint(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/push/vapid-public-key", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var resp struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Key == "" {
		t.Fatalf("response = %q", w.Body.String())
	}
	stored, ok, err := srv.db.KVGetString(context.Background(), pushVAPIDPublicKey)
	if err != nil || !ok || stored != resp.Key {
		t.Fatalf("stored key = %q ok=%v err=%v", stored, ok, err)
	}
}

func TestPushVAPIDPublicKeyRequiresOwner(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/push/vapid-public-key", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}
