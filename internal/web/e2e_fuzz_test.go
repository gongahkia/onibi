package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/coder/websocket"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/store"
)

func FuzzSequencedWSDecrypt(f *testing.F) {
	key := make([]byte, envelope.KeyBytes)
	client, err := newSeqWSClientCodec(key, "session", e2eInfoPTY, e2eDirS2C, e2eDirC2S)
	if err != nil {
		f.Fatal(err)
	}
	typ, sealed, err := client.encrypt(websocket.MessageText, []byte("seed"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(byte(typ), sealed)
	f.Fuzz(func(t *testing.T, rawType byte, frame []byte) {
		if len(frame) > 1<<20 {
			t.Skip()
		}
		server := newSeqWSCodec(key, "session", e2eInfoPTY, e2eDirC2S, e2eDirS2C)
		_, _, _ = server.decrypt(websocket.MessageType(rawType), frame)
	})
}

func FuzzPairConfirmRejectsAdversarialFrames(f *testing.F) {
	db, err := store.OpenEphemeral(filepath.Join(f.TempDir(), "onibi.db"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = db.Close() })
	srv := New(Options{DB: db})
	srv.requireE2E = true
	srv.relayKeys = NewRelayKeys()
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"v":"onibi.e2e.v1","sid":"unknown"}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > 1<<20 {
			t.Skip()
		}
		req := httptest.NewRequest(http.MethodPost, "/pair/confirm", bytes.NewReader(body))
		req.Header.Set("Content-Type", e2eContentType)
		w := httptest.NewRecorder()
		srv.handlePairConfirm(w, req)
		if w.Code < http.StatusBadRequest {
			t.Fatalf("adversarial pair status = %d", w.Code)
		}
	})
}
