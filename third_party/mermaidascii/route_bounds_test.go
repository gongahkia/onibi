package mermaidascii

import "testing"

func TestEnclosedTargetTerminates(t *testing.T) {
	l := &layout{occupied: map[gridPos]string{
		{1, 2}: "left", {3, 2}: "right", {2, 1}: "top", {2, 3}: "bottom",
	}}
	if path := l.findPath(gridPos{0, 0}, gridPos{2, 2}); path != nil {
		t.Fatal("path crossed a closed boundary", path)
	}
}

func TestBoundedSearchRetainsDetours(t *testing.T) {
	l := &layout{occupied: map[gridPos]string{{1, 0}: "wall"}}
	path := l.findPath(gridPos{0, 0}, gridPos{2, 0})
	if len(path) < 5 || path[0] != (gridPos{0, 0}) || path[len(path)-1] != (gridPos{2, 0}) {
		t.Fatal("missing detour", path)
	}
}
