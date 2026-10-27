package prompt

import (
	"reflect"
	"testing"
)

func TestDetectOffersChoicesThatMatchWhatAPersonWouldPress(t *testing.T) {
	for name, tc := range map[string]struct {
		screen string
		want   Prompt
	}{
		"yes/no":             {"Overwrite config.toml? [y/N] ", Prompt{Reason: "Yes/no prompt", Choices: []Choice{{Label: "Yes", Send: "y"}, {Label: "No", Send: "n"}}}},
		"key press":          {"Installed.\nPress Enter to continue", Prompt{Reason: "Waiting for a key press", Choices: []Choice{{Label: "Enter", Key: "enter"}}}},
		"password":           {"[sudo] password for me: ", Prompt{Reason: "Password prompt"}},
		"free-form question": {"Are you sure you want to continue? ", Prompt{Reason: "Question prompt"}},
		"boxed numbered menu": {`
╭──────────────────────────────────────────────╮
│ Do you want to proceed?                      │
│ ❯ 1. Yes                                     │
│   2. Yes, and don't ask again for ls commands │
│   3. No, and tell me what to do differently (esc) │
╰──────────────────────────────────────────────╯
 Esc to cancel · Tab to amend
`, Prompt{Reason: "Question prompt", Choices: []Choice{{Label: "Yes", Key: "1"}, {Label: "Yes, and don't ask again for ls commands", Key: "2"}, {Label: "No, and tell me what to do differently", Key: "esc"}}}},
		"menu above a confirm footer": {`
  Would you like to run the following command?
› 1. Yes, proceed (y)
  2. Yes, and don't ask again for this command (a)
  3. No (esc)

  Press enter to confirm or esc to cancel
`, Prompt{Reason: "Question prompt", Choices: []Choice{{Label: "Yes, proceed", Key: "y"}, {Label: "Yes, and don't ask again for this command", Key: "a"}, {Label: "No", Key: "esc"}}}},
		"unnumbered menu has no safe keys":  {"Do you want to proceed?\n❯ Yes\n  No\n", Prompt{Reason: "Question prompt"}},
		"repeated digits have no safe keys": {"Do you want to proceed?\n1. Yes\n1. No\n", Prompt{Reason: "Question prompt"}},
		"nothing waiting":                   {"$ ls\nREADME.md\n$ ", Prompt{}},
	} {
		if got := Detect(tc.screen); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %#v\nwant %#v", name, got, tc.want)
		}
	}
}

func TestLongChoiceLabelsAreClipped(t *testing.T) {
	got := Detect("Do you want to proceed?\n1. " + string(make([]byte, 0)) + "Yes, and also remember this decision for every command in every folder from now on\n2. No\n")
	if len(got.Choices) != 2 || len([]rune(got.Choices[0].Label)) != maxChoiceRunes {
		t.Fatalf("%#v", got)
	}
}
