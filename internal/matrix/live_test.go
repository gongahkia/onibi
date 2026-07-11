package matrix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/envelope"
	"github.com/gongahkia/onibi/internal/liveartifact"
	"github.com/gongahkia/onibi/internal/store"
)

func TestLiveMatrix(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_MATRIX") != "1" {
		t.Skip("set ONIBI_LIVE_MATRIX=1")
	}
	envs := []string{"ONIBI_MATRIX_HOMESERVER", "ONIBI_MATRIX_ACCESS_TOKEN", "ONIBI_MATRIX_ROOM_ID", "ONIBI_MATRIX_OWNER_USER_ID", "ONIBI_MATRIX_OWNER_DEVICE_ID", "ONIBI_MATRIX_SAS_VERIFIED"}
	rec, err := liveartifact.New("matrix", envs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rec.Close(envs...); err != nil {
			t.Errorf("artifact: %v", err)
		}
		t.Logf("artifact: %s", rec.Path())
	})
	c := New(os.Getenv("ONIBI_MATRIX_HOMESERVER"), os.Getenv("ONIBI_MATRIX_ACCESS_TOKEN"))
	roomID := os.Getenv("ONIBI_MATRIX_ROOM_ID")
	if roomID == "" {
		t.Fatal("ONIBI_MATRIX_ROOM_ID required")
	}
	who, err := c.CheckRoomOwner(t.Context(), roomID, 50)
	if err != nil {
		rec.Error("room-owner", err)
		t.Fatal(err)
	}
	rec.Record("room-owner", map[string]any{"user_id": who.UserID})
	encrypted, err := c.IsEncryptedRoom(t.Context(), roomID)
	if err != nil {
		rec.Error("encryption-state", err)
		t.Fatal(err)
	}
	rec.Record("encryption-state", map[string]any{"encrypted": encrypted})
	if _, err := c.Sync(t.Context(), "", time.Second); err != nil {
		rec.Error("sync", err)
		t.Fatal(err)
	}
	rec.Record("sync", map[string]any{"ok": true})
	if os.Getenv("ONIBI_LIVE_MATRIX_E2EE") == "1" {
		liveMatrixE2EE(t, rec, c, roomID, who)
	}
}

func liveMatrixE2EE(t *testing.T, rec *liveartifact.Recorder, c *Client, roomID string, who WhoAmI) {
	t.Helper()
	ownerUserID := strings.TrimSpace(os.Getenv("ONIBI_MATRIX_OWNER_USER_ID"))
	ownerDeviceID := strings.TrimSpace(os.Getenv("ONIBI_MATRIX_OWNER_DEVICE_ID"))
	deviceID := strings.TrimSpace(who.DeviceID)
	if deviceID == "" {
		deviceID = strings.TrimSpace(os.Getenv("ONIBI_MATRIX_DEVICE_ID"))
	}
	if ownerUserID == "" || ownerDeviceID == "" || deviceID == "" {
		t.Fatal("ONIBI_MATRIX_OWNER_USER_ID, ONIBI_MATRIX_OWNER_DEVICE_ID, and device_id/ONIBI_MATRIX_DEVICE_ID required for E2EE live smoke")
	}
	if !liveMatrixSASVerified() {
		t.Fatal("ONIBI_MATRIX_SAS_VERIFIED=1 required after manually verifying owner device SAS before E2EE live smoke")
	}
	encrypted, err := c.IsEncryptedRoom(t.Context(), roomID)
	if err != nil {
		rec.Error("e2ee-encryption-state", err)
		t.Fatal(err)
	}
	if !encrypted {
		t.Fatal("E2EE live smoke requires an encrypted Matrix room")
	}
	key, err := envelope.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "matrix-live-e2ee.sqlite"), store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	state, pickleKey, _, err := EnsureCryptoState(t.Context(), db, who.UserID, deviceID, 10)
	if err != nil {
		rec.Error("e2ee-crypto-state", err)
		t.Fatal(err)
	}
	state, upload, err := c.UploadCryptoKeys(t.Context(), state, pickleKey, !state.AccountShared)
	if err != nil {
		rec.Error("e2ee-keys-upload", err)
		t.Fatal(err)
	}
	rec.Record("e2ee-keys-upload", map[string]any{"one_time_key_counts": upload.OneTimeKeyCounts})
	state, err = MarkDeviceTrusted(state, ownerUserID, ownerDeviceID)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record("e2ee-sas-verified", map[string]any{"owner_user_id": ownerUserID, "owner_device_id": ownerDeviceID})
	outbound, roomKey, err := NewMegolmOutboundState(roomID, pickleKey)
	if err != nil {
		t.Fatal(err)
	}
	state.MegolmOutboundSessions = map[string]MegolmOutboundState{roomID: outbound}
	state, outbound, err = c.ShareRoomKeyWithTrustedUsers(t.Context(), state, outbound, roomKey, pickleKey, []string{ownerUserID}, 10*time.Second)
	if err != nil {
		rec.Error("e2ee-room-key-share", err)
		t.Fatal(err)
	}
	state.MegolmOutboundSessions[roomID] = outbound
	if err := SaveCryptoState(t.Context(), db, state); err != nil {
		t.Fatal(err)
	}
	rec.Record("e2ee-room-key-share", map[string]any{"shared_with": outbound.SharedWith})
	senderKey := strings.TrimSpace(state.DeviceKeys.Keys["curve25519:"+state.DeviceID])
	outbound, encryptedContent, err := EncryptMegolmRoomEvent(outbound, pickleKey, senderKey, state.DeviceID, roomID, "m.room.message", RoomMessage{MsgType: "m.text", Body: "onibi live encrypted smoke " + time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	eventID, err := c.SendMegolmEncryptedEvent(t.Context(), roomID, encryptedContent)
	if err != nil {
		rec.Error("e2ee-send", err)
		t.Fatal(err)
	}
	state.MegolmOutboundSessions[roomID] = outbound
	if err := SaveCryptoState(t.Context(), db, state); err != nil {
		t.Fatal(err)
	}
	rec.Record("e2ee-send", map[string]any{"event_id": eventID, "session_id": outbound.SessionID, "message_index": outbound.MessageIndex})
}

func liveMatrixSASVerified() bool {
	return strings.TrimSpace(os.Getenv("ONIBI_MATRIX_SAS_VERIFIED")) == "1"
}

func TestLiveMatrixSASVerifiedGate(t *testing.T) {
	t.Setenv("ONIBI_MATRIX_SAS_VERIFIED", "")
	if liveMatrixSASVerified() {
		t.Fatal("empty SAS gate passed")
	}
	t.Setenv("ONIBI_MATRIX_SAS_VERIFIED", "true")
	if liveMatrixSASVerified() {
		t.Fatal("non-1 SAS gate passed")
	}
	t.Setenv("ONIBI_MATRIX_SAS_VERIFIED", "1")
	if !liveMatrixSASVerified() {
		t.Fatal("SAS gate rejected 1")
	}
}
