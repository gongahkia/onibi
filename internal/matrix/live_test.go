package matrix

import (
	"os"
	"testing"
	"time"
)

func TestLiveMatrix(t *testing.T) {
	if os.Getenv("ONIBI_LIVE_MATRIX") != "1" {
		t.Skip("set ONIBI_LIVE_MATRIX=1")
	}
	c := New(os.Getenv("ONIBI_MATRIX_HOMESERVER"), os.Getenv("ONIBI_MATRIX_ACCESS_TOKEN"))
	roomID := os.Getenv("ONIBI_MATRIX_ROOM_ID")
	if roomID == "" {
		t.Fatal("ONIBI_MATRIX_ROOM_ID required")
	}
	if _, err := c.CheckRoomOwner(t.Context(), roomID, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Sync(t.Context(), "", time.Second); err != nil {
		t.Fatal(err)
	}
}
