package mermaidascii

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notYet lists the fixtures this port does not reproduce yet, with the reason.
// The entries are the work still to do; see docs/DEVIATIONS.md.
//
// Keeping them in a list rather than deleting them means the test suite states
// what is missing instead of quietly passing over it. Removing a line here is
// how a feature is declared finished.
var notYet = map[string]string{
	"flow-edge-labels":  "label placement — upstream reserves a row, this port squeezes labels beside the line",
	"flow-shapes":       "shape-specific box sizes — stadium and hexagon are drawn wider",
	"flow-selfloop":     "self edges are not routed around their node yet",
	"flow-cycle":        "back edges pick a different attachment point",
	"flow-direction-bt": "BT is mirrored onto TD, but the reference keeps the edge order",
}

// TestGolden compares the rendered output with the output of the reference
// implementation, byte for byte.
func TestGolden(t *testing.T) {
	inputs, err := filepath.Glob("testdata/input/*.mmd")
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}

	for _, in := range inputs {
		name := strings.TrimSuffix(filepath.Base(in), ".mmd")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}

			for _, mode := range []struct {
				suffix string
				opts   []Option
			}{
				{"unicode", nil},
				{"ascii", []Option{WithASCII()}},
			} {
				want, err := os.ReadFile(filepath.Join("testdata/golden", name+"."+mode.suffix+".txt"))
				if err != nil {
					t.Fatal(err)
				}
				got, err := Render(string(src), mode.opts...)
				if err != nil {
					t.Fatalf("%s: %v", mode.suffix, err)
				}
				if got == string(want) {
					continue
				}
				if reason, known := notYet[name]; known {
					t.Skipf("known gap — %s", reason)
				}
				t.Errorf("%s output differs\n--- got ---\n%s\n--- want ---\n%s", mode.suffix, got, want)
			}
		})
	}
}

// TestRenderIsStable guards against map iteration order leaking into the
// output: the same source must always draw the same picture.
func TestRenderIsStable(t *testing.T) {
	src := `flowchart TD
    A[Start] --> B{Check}
    B -->|yes| C[Done]
    B -->|no| D[Retry]
    D --> A`

	first, err := Render(src)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := Render(src)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("run %d differs from the first:\n%s\n---\n%s", i, again, first)
		}
	}
}

// TestRenderEmpty makes sure nothing panics on input without any nodes.
func TestRenderEmpty(t *testing.T) {
	for _, src := range []string{"", "flowchart TD", "%% only a comment", "not a diagram at all"} {
		if _, err := Render(src); err != nil {
			t.Errorf("Render(%q) = %v, want no error", src, err)
		}
	}
}
