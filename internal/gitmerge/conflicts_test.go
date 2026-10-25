package gitmerge

import (
	"context"
	"github.com/gongahkia/shack/internal/testutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkerStylesAndResolutions(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		for _, width := range []int{7, 10} {
			for _, base := range []bool{false, true} {
				lines := []string{"Heading", "=======", strings.Repeat("<", width) + " ours", "ours 😀"}
				if base {
					lines = append(lines, strings.Repeat("|", width)+" base", "base α")
				}
				lines = append(lines, strings.Repeat("=", width), "theirs 🦦", strings.Repeat(">", width)+" theirs", "tail")
				source := strings.Join(lines, newline)
				hunks, err := Parse(source)
				if err != nil || len(hunks) != 1 {
					t.Fatal(err, hunks)
				}
				h := hunks[0]
				for side, want := range map[string]string{"ours": "ours 😀" + newline, "theirs": "theirs 🦦" + newline, "both": "ours 😀" + newline + "theirs 🦦" + newline} {
					got, e := h.Resolve(source, side)
					if e != nil || got != want {
						t.Fatal(side, got, e)
					}
				}
				got, e := h.Resolve(source, "base")
				if base {
					if e != nil || got != "base α"+newline {
						t.Fatal(got, e)
					}
				} else if e == nil {
					t.Fatal("invented a base section")
				}
				if source[h.End:] != "tail" || h.Line != 2 {
					t.Fatal("range includes unrelated lines")
				}
			}
		}
	}
}
func TestRejectMalformedAndBoundedConflicts(t *testing.T) {
	for _, source := range []string{"<<<<<<< ours\nx\n", "<<<<<<< a\n<<<<<<< b\n=======\nx\n>>>>>>> y\n", "<<<<<<< a\nx\n========\ny\n>>>>>>> b\n", ">>>>>>> orphan\n"} {
		if _, err := Parse(source); err == nil {
			t.Fatal(source)
		}
	}
	if _, err := Parse(strings.Repeat("x", MaxBytes+1)); err == nil {
		t.Fatal("unbounded text")
	}
	if _, err := Parse(strings.Repeat("<<<<<<< a\nx\n=======\ny\n>>>>>>> b\n", MaxConflicts+1)); err == nil {
		t.Fatal("unbounded hunks")
	}
}
func TestRealGitStagesAndResolution(t *testing.T) {
	dir := testutil.ConflictedRepository(t)
	ctx := context.Background()
	r, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := r.Conflicts(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	entry := entries[0]
	for stage, want := range map[int]string{1: "base α", 2: "ours 😀", 3: "theirs 🦦"} {
		source, e := r.Read(ctx, entry, stage)
		if e != nil || !strings.Contains(source, want) {
			t.Fatal(stage, source, e)
		}
	}
	path, _ := r.Path(entry)
	data, _ := os.ReadFile(path)
	source := string(data)
	hunks, err := Parse(source)
	if err != nil || len(hunks) != 1 || !hunks[0].HasBase {
		t.Fatal(err, hunks)
	}
	value, _ := hunks[0].Resolve(source, "both")
	source = source[:hunks[0].Start] + value + source[hunks[0].End:]
	if err = os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.Stage(ctx, entry); err != nil {
		t.Fatal(err)
	}
	entries, err = r.Conflicts(ctx)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	if err = r.Current(ctx, entry); err == nil {
		t.Fatal("stale index snapshot accepted")
	}
	staged := testutil.Git(t, dir, "show", ":conflict file.txt")
	if staged != "header\nours 😀\ntheirs 🦦\nfooter\n" {
		t.Fatal(staged)
	}
	if _, err = os.Stat(filepath.Join(dir, ".git/MERGE_HEAD")); err != nil {
		t.Fatal("merge was unexpectedly committed/aborted")
	}
}
