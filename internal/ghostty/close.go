package ghostty

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

var ErrCloseScopeChanged = errors.New("the panes in this destination changed; review the close confirmation again. Nothing was closed")

func IsClose(kind string) bool {
	return kind == "close_pane" || kind == "close_tab" || kind == "close_window"
}

// CloseTargets is the set of existing pane identities inside the requested
// scope. Labels, ordering, and focus do not determine closure authority.
func CloseTargets(a Action, targets []Target) []string {
	var ids []string
	for _, t := range targets {
		if a.Kind == "close_pane" && t.ID == a.TargetID ||
			a.Kind == "close_tab" && t.WindowID == a.WindowID && t.TabID == a.TabID ||
			a.Kind == "close_window" && t.WindowID == a.WindowID {
			ids = append(ids, t.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func CheckCloseScope(a Action, targets []Target) error {
	if !IsClose(a.Kind) {
		return nil
	}
	if err := validateCloseScope(a); err != nil {
		return err
	}
	expected := slices.Clone(a.ExpectedTargets)
	slices.Sort(expected)
	if !slices.Equal(expected, CloseTargets(a, targets)) {
		return ErrCloseScopeChanged
	}
	return nil
}

func validateCloseScope(a Action) error {
	if !IsClose(a.Kind) {
		if len(a.ExpectedTargets) != 0 {
			return errors.New("expected targets are only valid for close actions")
		}
		return nil
	}
	if len(a.ExpectedTargets) == 0 || len(a.ExpectedTargets) > 256 {
		return errors.New("review the exact panes before closing (1–256 identities required)")
	}
	seen := make(map[string]bool, len(a.ExpectedTargets))
	for _, id := range a.ExpectedTargets {
		if id == "" || len(id) > 1024 || !utf8.ValidString(id) || strings.ContainsAny(id, "\x00\r\n") || seen[id] {
			return errors.New("close confirmation contains an invalid or duplicate pane identity")
		}
		seen[id] = true
	}
	if a.Kind == "close_pane" && (len(a.ExpectedTargets) != 1 || a.ExpectedTargets[0] != a.TargetID) {
		return errors.New("pane close confirmation must match the selected pane")
	}
	return nil
}
