package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func TestProjectAddHereSeedsCurrentRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "my-repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	out := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	if err := runProjectAdd(cmd, []string{"here"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Project my-repo saved") {
		t.Fatalf("out = %q", out.String())
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, ok, err := db.KVGetString(context.Background(), cliProjectAliasKey("my-repo"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != repo {
		t.Fatalf("alias = %q ok=%v", got, ok)
	}
}
