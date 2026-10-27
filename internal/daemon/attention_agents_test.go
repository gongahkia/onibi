package daemon

import "testing"

// Approval dialogs as coding agents draw them. Nothing here is agent-aware:
// they are generic question-and-menu layouts with borders and key hints.
func TestDetectPromptInAgentStyleDialogs(t *testing.T) {
	for name, screen := range map[string]string{
		"boxed dialog": `
╭───────────────────────────────────────────────────╮
│ Bash command                                      │
│                                                   │
│   ls -la                                          │
│   List files                                      │
│                                                   │
│ Do you want to proceed?                           │
│ ❯ 1. Yes                                          │
│   2. Yes, and don't ask again for ls commands     │
│   3. No, and tell Claude what to do differently   │
╰───────────────────────────────────────────────────╯
`,
		"dialog with key hint footer": `
 Bash command

   rm -rf build

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands in /tmp
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel · Tab to amend
`,
		"command approval with confirm hint": `
  Would you like to run the following command?

  $ git push

› 1. Yes, proceed (y)
  2. Yes, and don't ask again for this command (a)
  3. No, and tell Codex what to do differently (esc)

  Press enter to confirm or esc to cancel
`,
	} {
		if got := detectPrompt(screen); got == "" {
			t.Errorf("%s: no prompt detected", name)
		}
	}
	for name, screen := range map[string]string{
		"finished answer": `
⏺ Done. I ran ls and there are 3 files.

╭───────────────────────────────────────────────────╮
│ >                                                 │
╰───────────────────────────────────────────────────╯
  ? for shortcuts
`,
		"numbered list in prose": `
Steps:
1. Build
2. Test
Would you like to know more? Ask anytime.
$ `,
	} {
		if got := detectPrompt(screen); got != "" {
			t.Errorf("%s: false prompt %q", name, got)
		}
	}
}
