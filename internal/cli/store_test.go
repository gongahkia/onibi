package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/store"
)

func TestStoreRekeyCommandKeepsDevicesReadable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, db, err := openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(context.Background(), "cookie-value", "iPhone", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, _ := executeRoot(t, "store", "rekey", "--json", "--color", "never")
	var rekeyed struct {
		Rekeyed bool `json:"rekeyed"`
	}
	if err := json.Unmarshal(out.Bytes(), &rekeyed); err != nil {
		t.Fatal(err)
	}
	if !rekeyed.Rekeyed {
		t.Fatalf("rekey output = %q", out.String())
	}
	out, _ = executeRoot(t, "devices", "--json", "--color", "never")
	if !json.Valid(out.Bytes()) {
		t.Fatalf("devices output is not JSON: %q", out.String())
	}
}

func TestDevicesShowsRoleAndUnpairViewerScopes(t *testing.T) {
	withDefaultState(t)
	_, db, err := openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(context.Background(), "owner-cookie", "Mac", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSessionWithRole(context.Background(), "viewer-cookie", "Viewer", store.PairRoleViewer, time.Unix(11, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, _ := executeRoot(t, "devices", "--color", "never")
	got := out.String()
	if !strings.Contains(got, "ROLE") || !strings.Contains(got, "owner") || !strings.Contains(got, "viewer") {
		t.Fatalf("devices output missing roles:\n%s", got)
	}
	_, _, err = executeRootAllowError(t, "unpair", "--viewer", "owner-cookie", "--color", "never")
	if err == nil {
		t.Fatal("viewer-scoped unpair revoked owner")
	}
	out, _ = executeRoot(t, "unpair", "--viewer", "viewer-cookie", "--color", "never")
	if !strings.Contains(out.String(), "Revoked viewer-cookie") {
		t.Fatalf("unpair output = %q", out.String())
	}
	_, db, err = openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, ok, err := db.WebSession(context.Background(), "owner-cookie")
	if err != nil || !ok || owner.Revoked {
		t.Fatalf("owner session ok=%v err=%v session=%#v", ok, err, owner)
	}
	viewer, ok, err := db.WebSession(context.Background(), "viewer-cookie")
	if err != nil || !ok || !viewer.Revoked {
		t.Fatalf("viewer session ok=%v err=%v session=%#v", ok, err, viewer)
	}
}

func TestUnpairAllViewersLeavesOwners(t *testing.T) {
	withDefaultState(t)
	_, db, err := openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(context.Background(), "owner-cookie", "Mac", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"viewer-1", "viewer-2"} {
		if err := db.PutWebSessionWithRole(context.Background(), id, "Viewer", store.PairRoleViewer, time.Unix(11, 0)); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	out, _ := executeRoot(t, "unpair", "--all-viewers", "--color", "never")
	if !strings.Contains(out.String(), "Revoked 2 viewer device(s)") {
		t.Fatalf("unpair output = %q", out.String())
	}
	_, db, err = openCLIStore()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, ok, err := db.WebSession(context.Background(), "owner-cookie")
	if err != nil || !ok || owner.Revoked {
		t.Fatalf("owner session ok=%v err=%v session=%#v", ok, err, owner)
	}
	for _, id := range []string{"viewer-1", "viewer-2"} {
		viewer, ok, err := db.WebSession(context.Background(), id)
		if err != nil || !ok || !viewer.Revoked {
			t.Fatalf("%s ok=%v err=%v session=%#v", id, ok, err, viewer)
		}
	}
}
