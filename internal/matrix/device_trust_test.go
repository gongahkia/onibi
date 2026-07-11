package matrix

import (
	"strings"
	"testing"
)

func TestMarkDeviceTrustedFromCompletedSAS(t *testing.T) {
	state := CryptoState{SASTransactions: map[string]SASTransactionState{
		"txn-1": {TransactionID: "txn-1", UserID: "@alice:example", DeviceID: "ALICE", State: SASStateDone},
		"txn-2": {TransactionID: "txn-2", UserID: "@bob:example", DeviceID: "BOB", State: SASStateMACReceived},
	}}
	next, err := MarkDeviceTrustedFromSAS(state, "txn-1")
	if err != nil {
		t.Fatal(err)
	}
	if !IsDeviceTrusted(next, "@alice:example", "ALICE") {
		t.Fatalf("trusted devices = %#v", next.TrustedDevices)
	}
	if _, err := MarkDeviceTrustedFromSAS(state, "txn-2"); err == nil || !strings.Contains(err.Error(), "not complete") {
		t.Fatalf("err = %v", err)
	}
	if _, err := MarkDeviceTrustedFromSAS(state, "missing"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestTrustedRoomKeyShareTargetsFiltersUntrustedDevices(t *testing.T) {
	state := CryptoState{TrustedDevices: map[string][]string{"@alice:example": {"PHONE"}}}
	targets, err := TrustedRoomKeyShareTargets(state, []RoomKeyShareTarget{
		{UserID: "@alice:example", DeviceID: "LAPTOP"},
		{UserID: "@alice:example", DeviceID: "PHONE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].DeviceID != "PHONE" {
		t.Fatalf("targets = %#v", targets)
	}
	if _, err := TrustedRoomKeyShareTargets(state, []RoomKeyShareTarget{{UserID: "@alice:example", DeviceID: "LAPTOP"}}); err == nil || !strings.Contains(err.Error(), "trusted devices") {
		t.Fatalf("err = %v", err)
	}
}
