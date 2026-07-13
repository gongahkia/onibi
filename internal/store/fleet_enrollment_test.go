package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
)

func TestFleetOwnerIDIsStableAndRaceSafe(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const readers = 12
	ids := make(chan string, readers)
	errs := make(chan error, readers)
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := db.FleetOwnerID(context.Background())
			ids <- id
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	var first string
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("owner ids differ: %q != %q", id, first)
		}
	}
}

func TestFleetEnrollmentConsumesNonceOnlyOnce(t *testing.T) {
	db, err := OpenEphemeral(t.TempDir() + "/fleet.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	host := testFleetHost()
	host.State = fleet.HostStatePending
	challenge := fleet.EnrollmentChallenge{Version: fleet.ProtocolVersion, ID: "enroll-123", OwnerID: host.OwnerID, Nonce: "nonce", HubPublic: "hub-public", ExpiresAt: time.Now().Add(time.Minute)}
	if err := db.FleetEnrollmentIssue(context.Background(), challenge, host); err != nil {
		t.Fatal(err)
	}
	got, ok, err := db.FleetEnrollmentGet(context.Background(), challenge.ID)
	if err != nil || !ok || got.Host.ID != host.ID || !got.Challenge.ExpiresAt.Equal(challenge.ExpiresAt.Truncate(time.Second)) {
		t.Fatalf("enrollment=%#v ok=%v err=%v", got, ok, err)
	}
	if ok, err := db.FleetEnrollmentConsume(context.Background(), challenge.ID, "bad"); err != nil || ok {
		t.Fatalf("bad nonce ok=%v err=%v", ok, err)
	}
	if ok, err := db.FleetEnrollmentConsume(context.Background(), challenge.ID, challenge.Nonce); err != nil || !ok {
		t.Fatalf("first consume ok=%v err=%v", ok, err)
	}
	if ok, err := db.FleetEnrollmentConsume(context.Background(), challenge.ID, challenge.Nonce); err != nil || ok {
		t.Fatalf("replay consume ok=%v err=%v", ok, err)
	}
}
