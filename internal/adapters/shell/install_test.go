package shell

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewReportsPathsNotesAndThreshold(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct {
		name string
		path string
		note string
		body string
	}{
		{"zsh", filepath.Join(home, ".zshrc"), "oh-my-zsh", "add-zsh-hook preexec"},
		{"bash", filepath.Join(home, ".bashrc"), "PROMPT_COMMAND", "trap '__onibi_preexec' DEBUG"},
		{"fish", filepath.Join(home, ".config", "fish", "conf.d", "onibi.fish"), "conf.d", "fish_postexec"},
	}
	for _, c := range cases {
		got, err := Preview(c.name, "/tmp/onibi-notify", 12000)
		if err != nil {
			t.Fatal(err)
		}
		if got.Path != c.path || got.MinMS != 12000 {
			t.Fatalf("%s preview = %+v", c.name, got)
		}
		if !strings.Contains(got.Block, c.body) || !strings.Contains(strings.Join(got.CompatibilityNotes, "\n"), c.note) {
			t.Fatalf("%s preview missing body/note: %+v", c.name, got)
		}
		if !strings.Contains(got.EditCommand, "onibi config set shell.min_duration <duration>") {
			t.Fatalf("%s edit command = %q", c.name, got.EditCommand)
		}
	}
}
