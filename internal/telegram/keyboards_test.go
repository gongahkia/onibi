package telegram

import "testing"

func TestApprovalKeyboardLayout(t *testing.T) {
	kb := ApprovalKeyboard("abc123")
	if len(kb.InlineKeyboard) != 1 {
		t.Fatalf("expected 1 row, got %d", len(kb.InlineKeyboard))
	}
	row := kb.InlineKeyboard[0]
	if len(row) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(row))
	}
	want := []string{"Approve", "Deny", "Edit"}
	for i, b := range row {
		if b.Text != want[i] {
			t.Fatalf("button %d text = %q want %q", i, b.Text, want[i])
		}
		if len(b.CallbackData) > 64 {
			t.Fatalf("callback_data over 64 bytes (Telegram limit): %d", len(b.CallbackData))
		}
	}
}

func TestParseCallback(t *testing.T) {
	cases := []struct {
		in   string
		verb string
		id   string
	}{
		{"approve:abc123", "approve", "abc123"},
		{"deny:def456", "deny", "def456"},
		{"edit:ghi789", "edit", "ghi789"},
		{"target:s1", "target", "s1"},
		{"bogus:x", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		v, id := ParseCallback(c.in)
		if v != c.verb || id != c.id {
			t.Errorf("ParseCallback(%q) = (%q, %q), want (%q, %q)", c.in, v, id, c.verb, c.id)
		}
	}
}
