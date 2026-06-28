package matrix

import (
	"os"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/liveartifact"
)

func TestLiveMatrix(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_MATRIX") != "1" {
		t.Skip("set ONIBI_LIVE_MATRIX=1")
	}
	envs := []string{"ONIBI_MATRIX_HOMESERVER", "ONIBI_MATRIX_ACCESS_TOKEN", "ONIBI_MATRIX_ROOM_ID", "ONIBI_MATRIX_OWNER_USER_ID"}
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
}
