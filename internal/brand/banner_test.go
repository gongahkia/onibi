package brand

import (
	"strings"
	"testing"
)

func TestRenderRespectsWidth(t *testing.T) {
	for _, width := range []int{16, 32, 60} {
		got := Render(width)
		if strings.TrimSpace(got) == "" {
			t.Fatalf("empty render for width %d", width)
		}
		for _, line := range strings.Split(got, "\n") {
			if len(line) > width {
				t.Fatalf("line width %d exceeds %d: %q", len(line), width, line)
			}
		}
	}
}

func TestRenderChangesSize(t *testing.T) {
	small := Render(20)
	large := Render(50)
	if strings.Count(small, "\n") >= strings.Count(large, "\n") {
		t.Fatalf("expected larger render to have more rows")
	}
}

func TestSmartWidthTracksTerminalColumns(t *testing.T) {
	cases := []struct {
		cols int
		max  int
	}{
		{24, 22},
		{40, 38},
		{80, 72},
		{160, 72},
	}
	for _, tc := range cases {
		got := smartWidth(tc.cols)
		if got > tc.max {
			t.Fatalf("smartWidth(%d) = %d, want <= %d", tc.cols, got, tc.max)
		}
		if got < minWidth {
			t.Fatalf("smartWidth(%d) = %d, want >= %d", tc.cols, got, minWidth)
		}
	}
	if smartWidth(40) >= smartWidth(120) {
		t.Fatalf("expected wider terminals to render wider logos")
	}
}
