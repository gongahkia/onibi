package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelRendersResponsiveDiscoverState(t *testing.T) {
	initial := newModel(context.Background(), nil)
	updated, _ := initial.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	view := updated.(model).View()
	if !strings.Contains(view, "kaypoh") || !strings.Contains(view, "Discover") {
		t.Fatalf("unexpected TUI view: %q", view)
	}
}
