//go:build !onibi_remote

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestMatrixReactionApprovesPendingApproval(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example", OwnerUserID: "@owner:example"}})
	id, _, err := d.Queue.Request(t.Context(), "s1", "claude", "Bash", `{"command":"pwd"}`)
	if err != nil {
		t.Fatal(err)
	}
	d.storeMatrixApprovalEvent(t.Context(), "$approval", id)
	var sent []string
	c := matrixTestClient(t, &sent)
	d.handleMatrixEvent(t.Context(), c, matrix.Event{
		EventID: "$reaction",
		Type:    "m.reaction",
		Sender:  "@owner:example",
		Content: json.RawMessage(`{"m.relates_to":{"rel_type":"m.annotation","event_id":"$approval","key":"✅"}}`),
	})
	got, err := d.Queue.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != approval.StateApproved {
		t.Fatalf("approval state = %s", got.State)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "Approval "+id+": approved") {
		t.Fatalf("sent = %#v", sent)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.reaction") || !auditHas(audit, "approval.decided") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMatrixTextInRoutesToPTYAndAuditsTail(t *testing.T) {
	r := &tmuxRunner{results: [][]byte{
		nil,
		nil,
		[]byte("$ pwd\n/tmp/onibi\n"),
		[]byte("$ pwd\n/tmp/onibi\n"),
		[]byte("$ pwd\n/tmp/onibi\n"),
	}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example", OwnerUserID: "@owner:example"}})
	s := NewSession("s1", "shell", "shell", nil, 0)
	s.Transport = "tmux"
	s.TmuxTarget = "onibi-s1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	var sent []string
	c := matrixTestClient(t, &sent)
	d.handleMatrixEvent(t.Context(), c, matrix.Event{
		EventID: "$msg",
		Type:    "m.room.message",
		Sender:  "@owner:example",
		Content: json.RawMessage(`{"msgtype":"m.text","body":"pwd"}`),
	})
	if !containsCall(r.calls, "send-keys", "-t", "onibi-s1", "-l", "--", "pwd") {
		t.Fatalf("missing tmux send: %#v", r.calls)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "/tmp/onibi") {
		t.Fatalf("sent = %#v", sent)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.text_in") || !auditHas(audit, "provider.matrix.tail_chunk") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMatrixEncryptedBypassAuditsPlaintextFallback(t *testing.T) {
	db := openDaemonTestDB(t)
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example", AllowEncrypted: true}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account/whoami"):
			writeMatrixJSON(t, w, matrix.WhoAmI{UserID: "@bot:example"})
		case strings.Contains(r.URL.Path, "/state/m.room.power_levels"):
			writeMatrixJSON(t, w, matrix.PowerLevels{Users: map[string]int{"@bot:example": 100}})
		case strings.Contains(r.URL.Path, "/state/m.room.encryption"):
			writeMatrixJSON(t, w, map[string]any{"algorithm": "m.megolm.v1.aes-sha2"})
		case strings.HasSuffix(r.URL.Path, "/sync"):
			writeMatrixJSON(t, w, map[string]any{"next_batch": "s1", "rooms": map[string]any{"join": map[string]any{"!room:example": map[string]any{}}}})
			cancel()
		default:
			t.Fatalf("unexpected matrix path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	err := d.runMatrixBridge(ctx, matrix.New(srv.URL, "tok"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runMatrixBridge err = %v", err)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.encrypted_bypass") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMatrixBridgeInitializesAndUploadsCryptoKeys(t *testing.T) {
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "matrix.sqlite"), store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example"}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var upload keysUploadProbe
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account/whoami"):
			writeMatrixJSON(t, w, matrix.WhoAmI{UserID: "@bot:example", DeviceID: "DEV"})
		case strings.Contains(r.URL.Path, "/state/m.room.power_levels"):
			writeMatrixJSON(t, w, matrix.PowerLevels{Users: map[string]int{"@bot:example": 100}})
		case strings.Contains(r.URL.Path, "/state/m.room.encryption"):
			w.WriteHeader(http.StatusNotFound)
			writeMatrixJSON(t, w, map[string]any{"errcode": "M_NOT_FOUND", "error": "not found"})
		case strings.HasSuffix(r.URL.Path, "/keys/upload"):
			if err := json.NewDecoder(r.Body).Decode(&upload.Request); err != nil {
				t.Fatal(err)
			}
			writeMatrixJSON(t, w, map[string]any{"one_time_key_counts": map[string]int{matrix.KeyAlgorithmSignedCurve255: len(upload.Request.OneTimeKeys)}})
		case strings.HasSuffix(r.URL.Path, "/sync"):
			writeMatrixJSON(t, w, map[string]any{"next_batch": "s1", "rooms": map[string]any{"join": map[string]any{"!room:example": map[string]any{}}}})
			cancel()
		default:
			t.Fatalf("unexpected matrix path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	if err := d.runMatrixBridge(ctx, matrix.New(srv.URL, "tok")); !errors.Is(err, context.Canceled) {
		t.Fatalf("runMatrixBridge err = %v", err)
	}
	if upload.Request.DeviceKeys == nil || upload.Request.DeviceKeys.UserID != "@bot:example" || upload.Request.DeviceKeys.DeviceID != "DEV" || upload.Request.DeviceKeys.Signatures["@bot:example"]["ed25519:DEV"] == "" || len(upload.Request.OneTimeKeys) != matrixOneTimeKeyCount {
		t.Fatalf("upload = %#v", upload.Request)
	}
	state, ok, err := matrix.LoadCryptoState(t.Context(), db)
	if err != nil || !ok {
		t.Fatalf("crypto state ok=%v err=%v", ok, err)
	}
	if !state.AccountShared || state.DeviceID != "DEV" || state.OneTimeKeyCounts[matrix.KeyAlgorithmSignedCurve255] != matrixOneTimeKeyCount {
		t.Fatalf("state = %#v", state)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.crypto_upload") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMatrixBridgeSharesRoomKeyToOwnerDevices(t *testing.T) {
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "matrix.sqlite"), store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pickleKey := []byte("owner-pickle-key")
	owner, err := matrix.NewOlmAccountState("@owner:example", "OWNER", pickleKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	ownerOTKs, err := matrix.OlmAccountOneTimeKeys(owner, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example", OwnerUserID: "@owner:example", AllowEncrypted: true}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var sent map[string]map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/account/whoami"):
			writeMatrixJSON(t, w, matrix.WhoAmI{UserID: "@bot:example", DeviceID: "DEV"})
		case strings.Contains(r.URL.Path, "/state/m.room.power_levels"):
			writeMatrixJSON(t, w, matrix.PowerLevels{Users: map[string]int{"@bot:example": 100}})
		case strings.Contains(r.URL.Path, "/state/m.room.encryption"):
			writeMatrixJSON(t, w, map[string]any{"algorithm": matrix.AlgorithmMegolmV1})
		case strings.HasSuffix(r.URL.Path, "/keys/upload"):
			writeMatrixJSON(t, w, map[string]any{"one_time_key_counts": map[string]int{matrix.KeyAlgorithmSignedCurve255: matrixOneTimeKeyCount}})
		case strings.HasSuffix(r.URL.Path, "/keys/query"):
			writeMatrixJSON(t, w, map[string]any{"device_keys": map[string]any{"@owner:example": map[string]any{"OWNER": owner.DeviceKeys}}})
		case strings.HasSuffix(r.URL.Path, "/keys/claim"):
			writeMatrixJSON(t, w, map[string]any{"one_time_keys": map[string]any{"@owner:example": map[string]any{"OWNER": map[string]any{"signed_curve25519:OWNER": map[string]any{"key": firstMatrixOneTimeKey(t, ownerOTKs)}}}}})
		case strings.Contains(r.URL.Path, "/sendToDevice/m.room.encrypted/"):
			var req struct {
				Messages map[string]map[string]json.RawMessage `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			sent = req.Messages
			writeMatrixJSON(t, w, map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/sync"):
			writeMatrixJSON(t, w, map[string]any{"next_batch": "s1", "rooms": map[string]any{"join": map[string]any{"!room:example": map[string]any{}}}})
			cancel()
		default:
			t.Fatalf("unexpected matrix path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	if err := d.runMatrixBridge(ctx, matrix.New(srv.URL, "tok")); !errors.Is(err, context.Canceled) {
		t.Fatalf("runMatrixBridge err = %v", err)
	}
	if sent["@owner:example"]["OWNER"] == nil {
		t.Fatalf("sent = %#v", sent)
	}
	state, ok, err := matrix.LoadCryptoState(t.Context(), db)
	if err != nil || !ok {
		t.Fatalf("crypto state ok=%v err=%v", ok, err)
	}
	if state.MegolmOutboundSessions["!room:example"].SharedWith["@owner:example"][0] != "OWNER" {
		t.Fatalf("state = %#v", state.MegolmOutboundSessions)
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.room_key_shared") {
		t.Fatalf("audit = %#v", audit)
	}
}

func TestMatrixPostTailEncryptsMegolmRoomMessages(t *testing.T) {
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "matrix.sqlite"), store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	state, pickleKey, _, err := matrix.EnsureCryptoState(t.Context(), db, "@bot:example", "DEV", 0)
	if err != nil {
		t.Fatal(err)
	}
	outbound, roomKey, err := matrix.NewMegolmOutboundState("!room:example", pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	senderKey := state.DeviceKeys.Keys["curve25519:DEV"]
	inbound, err := matrix.NewMegolmInboundState(roomKey, senderKey, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	state.MegolmOutboundSessions = map[string]matrix.MegolmOutboundState{"!room:example": outbound}
	if err := matrix.SaveCryptoState(t.Context(), db, state); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Matrix: MatrixOptions{RoomID: "!room:example", AllowEncrypted: true}})
	d.setMatrixEncryptedRoom(true)
	var encrypted matrix.MegolmEncryptedContent
	var sentPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentPath = r.URL.Path
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/send/m.room.encrypted/txn-1") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if strings.Contains(r.URL.Path, "/send/m.room.message/") {
			t.Fatalf("plaintext send path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&encrypted); err != nil {
			t.Fatal(err)
		}
		writeMatrixJSON(t, w, map[string]any{"event_id": "$encrypted"})
	}))
	t.Cleanup(srv.Close)
	c := matrix.New(srv.URL, "tok")
	c.TxnID = func() string { return "txn-1" }
	d.postMatrixTail(t.Context(), c, "s1", "secret")
	if encrypted.Ciphertext == "" || sentPath == "" {
		t.Fatalf("encrypted=%#v path=%q", encrypted, sentPath)
	}
	_, payload, index, err := matrix.DecryptMegolmRoomEvent(inbound, pickleKey, encrypted, "!room:example")
	if err != nil {
		t.Fatal(err)
	}
	if index != 0 || payload.Type != "m.room.message" {
		t.Fatalf("index=%d payload=%#v", index, payload)
	}
	var msg matrix.RoomMessage
	if err := json.Unmarshal(payload.Content, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.MsgType != "m.text" || msg.Body != "secret" {
		t.Fatalf("message=%#v", msg)
	}
	loaded, ok, err := matrix.LoadCryptoState(t.Context(), db)
	if err != nil || !ok {
		t.Fatalf("crypto state ok=%v err=%v", ok, err)
	}
	if loaded.MegolmOutboundSessions["!room:example"].MessageIndex != 1 {
		t.Fatalf("outbound = %#v", loaded.MegolmOutboundSessions["!room:example"])
	}
	audit, err := db.AuditAll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !auditHas(audit, "provider.matrix.tail_chunk") {
		t.Fatalf("audit = %#v", audit)
	}
}

type keysUploadProbe struct {
	Request matrix.KeysUploadRequest
}

func firstMatrixOneTimeKey(t *testing.T, keys map[string]string) string {
	t.Helper()
	for _, key := range keys {
		return key
	}
	t.Fatal("expected one-time key")
	return ""
}

func TestMatrixReactionVerdict(t *testing.T) {
	if matrixReactionVerdict("✅") != approval.VerdictApprove || matrixReactionVerdict("👍") != approval.VerdictApprove {
		t.Fatal("approve reaction mapping failed")
	}
	if matrixReactionVerdict("❌") != approval.VerdictDeny || matrixReactionVerdict("👎") != approval.VerdictDeny {
		t.Fatal("deny reaction mapping failed")
	}
	if matrixReactionVerdict("🙂") != "" {
		t.Fatal("unknown reaction mapped to verdict")
	}
}

func matrixTestClient(t *testing.T, sent *[]string) *matrix.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/send/m.room.message/") {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var msg matrix.RoomMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Fatal(err)
		}
		*sent = append(*sent, msg.Body)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(matrix.SendResponse{EventID: "$sent"}); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := matrix.New(srv.URL, "tok")
	c.TxnID = func() string { return "txn" }
	return c
}

func writeMatrixJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatal(err)
	}
}

func auditHas(entries []store.AuditEntry, action string) bool {
	for _, entry := range entries {
		if entry.Action == action {
			return true
		}
	}
	return false
}
