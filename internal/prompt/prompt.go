// Package prompt recognises terminal output that is waiting for a person:
// yes/no questions, key-press pauses, password prompts and selection menus.
// The patterns describe common terminal conventions rather than any
// particular program's interface.
package prompt

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	windowLines    = 8
	maxChoiceRunes = 60
)

// Choice is one reply a person could give. Exactly one of Key and Send is
// set: Key is a single key press, Send is text followed by Enter.
type Choice struct {
	Label string `json:"label"`
	Key   string `json:"key,omitempty"`
	Send  string `json:"send,omitempty"`
}

// Prompt describes output that is waiting for input. Choices is empty when
// the reply cannot be inferred safely, such as for passwords or free-form
// questions.
type Prompt struct {
	Reason  string   `json:"reason"`
	Choices []Choice `json:"choices,omitempty"`
}

// Cursor-line patterns must end the last non-empty line, so an answered
// prompt or a command line that merely contains "[y/N]" does not match.
var cursorPrompts = []struct {
	re      *regexp.Regexp
	reason  string
	choices []Choice
}{
	{regexp.MustCompile(`(?i)[\[(]\s*(y/n|yes/no)\s*[\])]\s*[:?>]?\s*$`), "Yes/no prompt", []Choice{{Label: "Yes", Send: "y"}, {Label: "No", Send: "n"}}},
	{regexp.MustCompile(`(?i)\bpress (enter|return|any key)\b[^\n]{0,40}$`), "Waiting for a key press", []Choice{{Label: "Enter", Key: "enter"}}},
	{regexp.MustCompile(`(?i)\b(password|passphrase|passcode)\b[^:]*:\s*$`), "Password prompt", nil},
	{regexp.MustCompile(`(?i)\b(do you want to|would you like to|are you sure|continue|proceed|overwrite)\b[^\n]*\?\s*$`), "Question prompt", nil},
}

// A question above the cursor counts only when every line below it looks
// like a choice in a selection menu, optionally followed by key hints such as
// "Esc to cancel". Box-drawing borders around a dialog are ignored.
var (
	menuQuestion = regexp.MustCompile(`(?i)\b(do you want to|would you like to|are you sure)\b[^\n]*\?`)
	menuChoice   = regexp.MustCompile(`(?i)^\s*([❯›>*•]\s*)?(\d+[.)]|yes\b|no\b|\[[ x]\])`)
	numbered     = regexp.MustCompile(`^\s*(?:[❯›>*•]\s*)?([1-9])[.)]\s*(\S.*)$`)
	advertised   = regexp.MustCompile(`(?i)\s*\(([a-z]|esc)\)$`)
	keyHint      = regexp.MustCompile(`(?i)^\s*(press\s+)?(esc|enter|return|tab|space|ctrl[-+]\S+)\s+to\b`)
)

const (
	boxSides   = "│┃║"
	boxBorders = "─━═╭╮╰╯┌┐└┘┏┓┗┛╔╗╚╝├┤┬┴┼"
)

// line removes dialog box edges; a border-only line becomes empty.
func line(s string) string {
	s = strings.Trim(s, " \t\r"+boxSides)
	if strings.Trim(s, " "+boxBorders) == "" {
		return ""
	}
	return s
}

// Detect inspects the last few non-empty lines of plain terminal text.
func Detect(plain string) Prompt {
	lines := strings.Split(strings.TrimRight(plain, " \t\r\n"), "\n")
	var recent []string
	for i := len(lines) - 1; i >= 0 && len(recent) < windowLines; i-- {
		if l := line(lines[i]); l != "" {
			recent = append(recent, l)
		}
	}
	if len(recent) == 0 {
		return Prompt{}
	}
	if menu, ok := detectMenu(recent); ok {
		return menu
	}
	for _, pattern := range cursorPrompts {
		if pattern.re.MatchString(recent[0]) {
			return Prompt{Reason: pattern.reason, Choices: pattern.choices}
		}
	}
	return Prompt{}
}

// detectMenu runs before the cursor-line patterns so a footer such as "Press
// enter to confirm" does not hide the choices above it.
func detectMenu(recent []string) (Prompt, bool) {
	var menu []string
	for _, l := range recent {
		if len(menu) == 0 && keyHint.MatchString(l) {
			continue
		}
		if !menuChoice.MatchString(l) {
			if len(menu) > 0 && menuQuestion.MatchString(l) {
				return Prompt{Reason: "Question prompt", Choices: menuChoices(menu)}, true
			}
			return Prompt{}, false
		}
		menu = append(menu, l)
	}
	return Prompt{}, false
}

// menuChoices offers keys only when every choice is numbered 1-9 with a
// distinct digit. A key the choice advertises, such as "(y)" or "(esc)", wins
// over its digit. Enter is never added because menus that confirm on the key
// would pass it to whatever comes next.
func menuChoices(bottomUp []string) []Choice {
	choices := make([]Choice, 0, len(bottomUp))
	seen := map[string]bool{}
	for i := len(bottomUp) - 1; i >= 0; i-- {
		m := numbered.FindStringSubmatch(bottomUp[i])
		if m == nil || seen[m[1]] {
			return nil
		}
		seen[m[1]] = true
		label, key := strings.TrimSpace(m[2]), m[1]
		if hint := advertised.FindStringSubmatch(label); hint != nil {
			label, key = strings.TrimSpace(strings.TrimSuffix(label, hint[0])), strings.ToLower(hint[1])
		}
		choices = append(choices, Choice{Label: clip(label), Key: key})
	}
	return choices
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxChoiceRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxChoiceRunes-1]) + "…"
}
