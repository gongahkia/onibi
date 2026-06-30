package cli

import (
	"context"
	"net/url"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func TestShareQuietHostPortOverride(t *testing.T) {
	withDefaultState(t)
	db, err := openDefaultDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SessionUpsertStart(context.Background(), "s1", "claude", "claude", "/tmp", "claude", "pty", "", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, _ := executeRoot(t, "share", "s1", "--quiet", "--host", "phone.local", "--port", "9443", "--ttl", "30m", "--max-viewers", "2", "--no-qr", "--color", "never")
	got := strings.TrimSpace(out.String())
	if !strings.HasPrefix(got, "https://phone.local:9443/pair/") || !strings.HasSuffix(got, "#/s/s1") {
		t.Fatalf("share url = %q", got)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	token := path.Base(u.Path)
	db, err = openDefaultDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	claim, ok, err := db.ClaimPairingToken(context.Background(), token)
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if claim.Role != store.PairRoleViewer || claim.SessionID != "s1" {
		t.Fatalf("claim = %#v", claim)
	}
}
