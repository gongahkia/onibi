package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"
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
