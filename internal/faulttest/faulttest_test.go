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
