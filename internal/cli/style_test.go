package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRenderTableWrapsToTerminalWidth(t *testing.T) {
	old := tableWidthForWriter
	tableWidthForWriter = func(io.Writer) int { return 54 }
	t.Cleanup(func() { tableWidthForWriter = old })

	var out bytes.Buffer
	err := renderTable(&out, [][]string{
		tableHeader(cliStyle{}, "#", "PROVIDER", "BEST FOR", "COMMAND"),
		{"2", "Tailscale Serve", "private tailnet HTTPS", "onibi start --transport=tailscale-private"},
		{"-", "Cloudflare Tunnel", "public web URL", "not in this build"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if visibleLen(line) > 54 {
			t.Fatalf("line width=%d line=%q\n%s", visibleLen(line), line, got)
		}
	}
	if !strings.Contains(got, "Tailscale") || !strings.Contains(got, "private") || !strings.Contains(got, "not in this") {
		t.Fatalf("wrapped output lost content:\n%s", got)
	}
}
