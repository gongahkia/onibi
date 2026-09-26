package daemon

import "testing"

func TestOutputScreenDimensionsFollowVisibleContent(t *testing.T) {
	rows, cols := outputScreenDimensions([]byte("\x1b[31mhello\x1b[0m\n12345678901234567890123456789012345678901234567890"))
	if rows < 16 || cols != 50 {
		t.Fatalf("rows=%d cols=%d", rows, cols)
	}
}
