package cli

import "testing"

func TestParseTurnArg(t *testing.T) {
	for _, raw := range []string{"@turn-3", "turn-3", "3"} {
		got, err := parseTurnArg(raw)
		if err != nil || got != 3 {
			t.Fatalf("%q -> %d err=%v", raw, got, err)
		}
	}
	for _, raw := range []string{"@turn-x", "-1"} {
		if _, err := parseTurnArg(raw); err == nil {
			t.Fatalf("%q did not fail", raw)
		}
	}
}
