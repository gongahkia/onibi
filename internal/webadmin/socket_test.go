package webadmin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/web"
)

type fakeAdmin struct{ revoked string }

func (f *fakeAdmin) RevokeAllDevices(context.Context) error { f.revoked = "all"; return nil }
func (f *fakeAdmin) FreshPairingURL() (string, error) {
	return "https://example.test/#token=secret", nil
}
func (f *fakeAdmin) ListDevices(context.Context) ([]web.Device, error) {
	return []web.Device{{ID: "device", Name: "Phone"}}, nil
}
func (f *fakeAdmin) RevokeDevice(_ context.Context, id string) error { f.revoked = id; return nil }
func TestLocalSocketLifecycleAndOperations(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "onibi-admin-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := Path(dir)
	admin := &fakeAdmin{}
	s, err := Listen(path, admin)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe socket: %v", err)
	}
	if other, err := Listen(path, admin); err == nil {
		other.Close()
		t.Fatal("replaced live socket")
	}
	for _, op := range []string{"pair", "devices", "revoke"} {
		response, err := Call(context.Background(), path, Request{Operation: op, ID: "test-device"})
		if err != nil {
			t.Fatal(err)
		}
		if op == "pair" && !strings.Contains(response.PairingURL, "#token=") {
			t.Fatal("no pairing link")
		}
		if op == "devices" && len(response.Devices) != 1 {
			t.Fatal("no devices")
		}
	}
	if admin.revoked != "test-device" {
		t.Fatal("revocation lost")
	}
	if _, err := Call(context.Background(), path, Request{Operation: "execute"}); err == nil {
		t.Fatal("accepted terminal operation")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("socket left after shutdown")
	}
	if err := os.WriteFile(path, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path, admin); err == nil {
		t.Fatal("replaced regular file")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "preserve" {
		t.Fatal("modified regular file")
	}
	os.Remove(path)
	if err := os.Symlink(filepath.Join(dir, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path, admin); err == nil {
		t.Fatal("replaced symlink")
	}
	os.Remove(path)
	os.Chmod(dir, 0755)
	if _, err := Listen(path, admin); err == nil {
		t.Fatal("accepted public directory")
	}
}
