package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/store"
)

func TestUpRunsSetupWhenUnpaired(t *testing.T) {
	withDefaultState(t)
	oldWebPair := webPairRun
	oldInstall := installServiceRun
	webPairCalled := false
	webPairRun = func(cmd *cobra.Command, _ config.Paths, _ *store.DB) error {
		webPairCalled = true
		cmd.Println("pair stub")
		return nil
	}
	installServiceRun = func(*cobra.Command, []string) error {
		t.Fatal("install service should not run")
		return nil
	}
	t.Cleanup(func() {
		webPairRun = oldWebPair
		installServiceRun = oldInstall
	})

	var out bytes.Buffer
	cmd := upCmd()
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !webPairCalled {
		t.Fatal("web pair not called")
	}
	if !strings.Contains(out.String(), "pair stub") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestUpInstallsServiceWhenPaired(t *testing.T) {
	paths := withDefaultState(t)
	db, err := store.Open(paths.DBFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.SetOwner(context.Background(), db, &auth.Owner{}, 123); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	oldInstall := installServiceRun
	oldDoctor := doctorRun
	installCalled := false
	doctorCalled := false
	installServiceRun = func(*cobra.Command, []string) error {
		installCalled = true
		return nil
	}
	doctorRun = func(context.Context, doctor.Options) doctor.Report {
		doctorCalled = true
		return doctor.Report{Checks: []doctor.Check{{Name: "ok", Status: doctor.Pass, Detail: "ok"}}}
	}
	t.Cleanup(func() {
		installServiceRun = oldInstall
		doctorRun = oldDoctor
	})

	var out bytes.Buffer
	cmd := upCmd()
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !installCalled || !doctorCalled {
		t.Fatalf("install=%v doctor=%v", installCalled, doctorCalled)
	}
	if !strings.Contains(out.String(), "Already paired") || !strings.Contains(out.String(), "[PASS] ok: ok") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func withDefaultState(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg-data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	return paths
}
