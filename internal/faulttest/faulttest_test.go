package faulttest

import (
	"errors"
	"testing"
)

func TestProcessKeepsFirstExitError(t *testing.T) {
	process := NewProcess()
	want := errors.New("process exit")
	process.Exit(want)
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); !errors.Is(err, want) {
		t.Fatalf("wait error=%v want=%v", err, want)
	}
}

func TestProviderRecordsEnablePorts(t *testing.T) {
	provider := &Provider{}
	if err := provider.Enable(t.Context(), 8443); err != nil {
		t.Fatal(err)
	}
	if err := provider.Enable(t.Context(), 9443); err != nil {
		t.Fatal(err)
	}
	if got := provider.Ports(); len(got) != 2 || got[0] != 8443 || got[1] != 9443 {
		t.Fatalf("ports=%#v", got)
	}
}
