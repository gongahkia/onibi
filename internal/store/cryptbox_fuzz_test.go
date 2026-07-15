package store

import (
	"context"
	"testing"
)

func FuzzCryptBoxOpen(f *testing.F) {
	key := make([]byte, 32)
	box, err := NewCryptBox(key)
	if err != nil {
		f.Fatal(err)
	}
	aad := RowAAD("web_sessions", "session", "device_label")
	sealed, err := box.Seal(context.Background(), []byte("secret"), aad)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(sealed, aad)
	f.Add([]byte(`{"v":"onibi.e2e.v1"}`), aad)
	f.Fuzz(func(t *testing.T, frame, associatedData []byte) {
		if len(frame) > 1<<20 || len(associatedData) > 1<<20 {
			t.Skip()
		}
		_, _ = box.Open(context.Background(), frame, associatedData)
	})
}
