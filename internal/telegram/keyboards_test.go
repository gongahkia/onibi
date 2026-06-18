package telegram

import (
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestApprovalKeyboardLayout(t *testing.T) {
	kb := ApprovalKeyboard("abc123")
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(kb.InlineKeyboard))
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
	if kb.InlineKeyboard[1][0].Text != "Reason" {
		t.Fatalf("reason row = %#v", kb.InlineKeyboard[1])
	}
}

func TestParseCallback(t *testing.T) {
	cases := []struct {
		in   string
		verb string
		id   string
	}{
		{"approve:abc123", "approve", "abc123"},
		{"confirm:abc123", "confirm_approve", "abc123"},
		{"deny:def456", "deny", "def456"},
		{"reason:def456", "deny_reason", "def456"},
		{"edit:ghi789", "edit", "ghi789"},
		{"target:s1", "target", "s1"},
		{"psend:p1", "prompt_send", "p1"},
		{"pedit:p1", "prompt_edit", "p1"},
		{"pcancel:p1", "prompt_cancel", "p1"},
		{"pup:p1", "prompt_up", "p1"},
		{"pdown:p1", "prompt_down", "p1"},
		{"ptop:p1", "prompt_top", "p1"},
		{"pflush:p1", "prompt_flush", "p1"},
		{"pconfirm:p1", "prompt_confirm_send", "p1"},
		{"mproj", "menu_projects", ""},
		{"mdoc", "menu_doctor", ""},
		{"mhooks", "menu_hooks", ""},
		{"msend:s1", "menu_send", "s1"},
		{"msnooze", "menu_snooze", ""},
		{"munsnooze", "menu_unsnooze", ""},
		{"mmenu", "menu_home", ""},
		{"obproj", "onboard_project", ""},
		{"obagent", "onboard_agent", ""},
		{"obvis", "onboard_visible", ""},
		{"obdemo", "demo_approval", ""},
		{"proj:repo", "project_alias", "repo"},
		{"pnew:visible:shell:repo", "project_start", "visible:shell:repo"},
		{"peek:s1", "peek", "s1"},
		{"render:s1", "render", "s1"},
		{"shot:s1", "render", "s1"},
		{"int:s1", "interrupt", "s1"},
		{"kill:s1", "kill", "s1"},
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

func TestOnboardingKeyboardHasGuidedActions(t *testing.T) {
	kb := OnboardingKeyboard()
	got := ""
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			got += b.Text + " "
			if len(b.CallbackData) > 64 {
				t.Fatalf("%s callback_data over 64 bytes: %d", b.Text, len(b.CallbackData))
			}
		}
	}
	for _, want := range []string{"Add Project", "Choose Agent", "Start Visible", "Test Approval", "Sessions", "Menu"} {
		if !strings.Contains(got, want) {
			t.Fatalf("keyboard missing %q: %s", want, got)
		}
	}
}

func TestSessionMenuCallbackDataLimit(t *testing.T) {
	kb := SessionMenuKeyboard([]SessionTarget{
		{ID: "0123456789abcdef", Label: "codex 012345", Selected: true},
	})
	for _, row := range kb.InlineKeyboard {
		for _, b := range row {
			if len(b.CallbackData) > 64 {
				t.Fatalf("%s callback_data over 64 bytes: %d", b.Text, len(b.CallbackData))
			}
		}
	}
}

func TestProjectKeyboardCallbackDataLimit(t *testing.T) {
	kbs := []*models.InlineKeyboardMarkup{
		ProjectAliasKeyboard([]string{"repo", strings.Repeat("x", 80)}),
		ProjectStartKeyboard("repo"),
	}
	for _, kb := range kbs {
		for _, row := range kb.InlineKeyboard {
			for _, b := range row {
				if len(b.CallbackData) > 64 {
					t.Fatalf("%s callback_data over 64 bytes: %d", b.Text, len(b.CallbackData))
				}
			}
		}
	}
}
