package ghostty

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Control is an intentionally bounded part of Ghostty's action dictionary.
// It is not a raw perform-action escape hatch. Destructive lifecycle operations
// have their own explicit, scoped requests and confirmations in the client.
type Control struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Group  string `json:"group"`
	Action string `json:"-"`
}

func Controls() []Control {
	return []Control{
		{"zoom", "Zoom / restore pane", "Layout", "toggle_split_zoom"},
		{"equalize", "Balance splits", "Layout", "equalize_splits"},
		{"resize_left", "Resize left", "Layout", "resize_split:left,40"},
		{"resize_right", "Resize right", "Layout", "resize_split:right,40"},
		{"resize_up", "Resize up", "Layout", "resize_split:up,40"},
		{"resize_down", "Resize down", "Layout", "resize_split:down,40"},
		{"scroll_top", "Oldest output", "Scrollback", "scroll_to_top"},
		{"scroll_up", "Page up", "Scrollback", "scroll_page_up"},
		{"scroll_down", "Page down", "Scrollback", "scroll_page_down"},
		{"scroll_bottom", "Latest output", "Scrollback", "scroll_to_bottom"},
		{"prompt_previous", "Previous prompt", "Scrollback", "jump_to_prompt:-1"},
		{"prompt_next", "Next prompt", "Scrollback", "jump_to_prompt:1"},
		{"font_increase", "Larger on Mac", "Mac display", "increase_font_size:1"},
		{"font_decrease", "Smaller on Mac", "Mac display", "decrease_font_size:1"},
		{"font_reset", "Reset Mac font", "Mac display", "reset_font_size"},
		{"fullscreen", "Toggle fullscreen", "Mac display", "toggle_fullscreen"},
	}
}

func controlAction(id string) (string, bool) {
	for _, control := range Controls() {
		if control.ID == id {
			return control.Action, true
		}
	}
	return "", false
}

// ValidateAction validates the public shape without interpreting native IDs.
// Both the HTTP boundary and the native client use the same action contract.
func ValidateAction(a Action) error {
	if err := validateCloseScope(a); err != nil {
		return err
	}
	for _, id := range []string{a.TargetID, a.WindowID, a.TabID} {
		if len(id) > 1024 || !utf8.ValidString(id) || strings.ContainsAny(id, "\x00\r\n") {
			return errors.New("invalid target identity")
		}
	}
	if a.Kind != "input" && (a.Text != "" || a.Enter) {
		return errors.New("text and Enter are only valid for input")
	}
	if a.Kind != "key" && a.Key != "" {
		return errors.New("keys are only valid for key actions")
	}
	if a.Kind != "split" && a.Direction != "" {
		return errors.New("direction is only valid for a split")
	}
	if a.Kind != "control" && a.Control != "" {
		return errors.New("control is only valid for a Ghostty control action")
	}
	if a.Kind != "rename_tab" && a.Kind != "rename_pane" && a.Name != "" {
		return errors.New("name is only valid for renaming")
	}
	switch a.Kind {
	case "new_window":
		if a.TargetID != "" || a.WindowID != "" || a.TabID != "" {
			return errors.New("a new window must not name an existing destination")
		}
	case "new_tab", "close_window":
		if a.WindowID == "" || a.TabID != "" || a.TargetID != "" {
			return errors.New("this action requires only an explicit window")
		}
	case "close_tab":
		if a.WindowID == "" || a.TabID == "" {
			return errors.New("closing a tab requires its explicit window and tab")
		}
	case "input", "key", "focus", "split", "close_pane", "rename_tab", "rename_pane", "control":
		if a.TargetID == "" {
			return errors.New("select an explicit pane")
		}
	default:
		return errors.New("unsupported Ghostty action")
	}
	if a.Kind == "input" && (a.Text == "" || len(a.Text) > 65536 || !utf8.ValidString(a.Text) || strings.ContainsRune(a.Text, 0)) {
		return errors.New("input must contain valid text without NUL characters (at most 64 KiB)")
	}
	if a.Kind == "key" {
		if _, _, err := nativeKey(a.Key); err != nil {
			return err
		}
	}
	if a.Kind == "split" && a.Direction != "right" && a.Direction != "left" && a.Direction != "up" && a.Direction != "down" {
		return errors.New("split direction must be right, left, up, or down")
	}
	if a.Kind == "control" {
		if _, ok := controlAction(a.Control); !ok {
			return errors.New("unsupported Ghostty control")
		}
	}
	if a.Kind == "rename_tab" || a.Kind == "rename_pane" {
		if len(a.Name) > 256 || !utf8.ValidString(a.Name) || strings.IndexFunc(a.Name, unicode.IsControl) >= 0 {
			return errors.New("titles must be at most 256 bytes without control characters")
		}
	}
	return nil
}
