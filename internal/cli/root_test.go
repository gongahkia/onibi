package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSourcesListJSONIsClean(t *testing.T) {
	database := t.TempDir() + "/courtsg.db"
	var stdout, stderr bytes.Buffer
	exit := Execute(context.Background(), "test", []string{"--db", database, "--json", "sources", "list"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %s", exit, stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") || !strings.HasPrefix(strings.TrimSpace(stdout.String()), "[") {
		t.Fatalf("unexpected JSON output: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestParseCents(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int64
	}{
		{input: "12", want: 1200},
		{input: "12.5", want: 1250},
		{input: "S$0.05", want: 5},
	} {
		got, err := parseCents(test.input)
		if err != nil || got != test.want {
			t.Fatalf("parseCents(%q) = %d, %v; want %d", test.input, got, err, test.want)
		}
	}
	if _, err := parseCents("12.345"); err == nil {
		t.Fatal("parseCents accepted more than two decimal places")
	}
}
