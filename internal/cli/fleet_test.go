package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/fleetnode"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
)

func TestFleetEndpointSelectsAdapter(t *testing.T) {
	out, _ := executeRoot(t, "fleet", "endpoint", "relay", "https://relay.example.test", "--json", "--color", "never")
	var got struct {
		Version  uint16 `json:"version"`
		Adapter  string `json:"adapter"`
		Endpoint struct {
			Kind string `json:"kind"`
			URL  string `json:"url"`
		} `json:"endpoint"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Adapter != "relay" || got.Endpoint.Kind != "relay" || got.Endpoint.URL != "https://relay.example.test" {
		t.Fatalf("fleet endpoint = %#v", got)
	}
}

func TestFleetEndpointRejectsInvalidAdapterInput(t *testing.T) {
	_, _, err := executeRootAllowError(t, "fleet", "endpoint", "ssh", "onibi@host.example.test", "--color", "never")
	if err == nil || !strings.Contains(err.Error(), "unsupported fleet enrollment adapter \"ssh\"") {
		t.Fatalf("fleet endpoint error = %v", err)
	}
}

func TestFleetEnrollRelayUsesOwnerAuthorizedHubFlow(t *testing.T) {
	withDefaultState(t)
	hubDB, err := store.OpenEphemeral(t.TempDir() + "/hub.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer hubDB.Close()
	hub := web.New(web.Options{DB: hubDB})
	ts := httptest.NewTLSServer(hub.Handler())
	defer ts.Close()
	rr := httptest.NewRecorder()
	owner, err := hub.CreateOwnerSession(context.Background(), rr, "owner")
	if err != nil {
		t.Fatal(err)
	}
	oldClient := newFleetHTTPClient
	newFleetHTTPClient = func() *http.Client { return ts.Client() }
	t.Cleanup(func() { newFleetHTTPClient = oldClient })
	out, _ := executeRoot(t, "fleet", "enroll", "--hub", ts.URL, "--endpoint", "https://relay.example.test", "--owner-session", owner, "--color", "never")
	if !strings.Contains(out.String(), "Enrolled relay fleet host") {
		t.Fatalf("output=%q", out.String())
	}
	db, err := openDefaultDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, found, err := fleetnode.LoadConfig(context.Background(), db); err != nil || !found {
		t.Fatalf("fleet config found=%v err=%v", found, err)
	}
	hosts, err := hubDB.FleetHostList(context.Background())
	if err != nil || len(hosts) != 1 || hosts[0].Endpoint.URL != "https://relay.example.test" {
		t.Fatalf("hosts=%#v err=%v", hosts, err)
	}
}
