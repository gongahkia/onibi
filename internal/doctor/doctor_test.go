package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web/transport"
)

func TestDoctorFlagsMissingStoreKey(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "store key")
	if check.Status != Fail {
		t.Fatalf("store key check = %#v", check)
	}
	if !strings.Contains(check.Detail, "missing") {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func TestDoctorPassesDecryptableStoreKey(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	key, err := secrets.GetOrCreateStoreKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutWebSession(context.Background(), "cookie", "iPhone", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	report := Run(context.Background(), Options{Paths: paths})
	if check := checkNamed(t, report, "store key"); check.Status != Pass {
		t.Fatalf("store key check = %#v", check)
	}
	if check := checkNamed(t, report, "sqlite db"); check.Status != Pass {
		t.Fatalf("sqlite check = %#v", check)
	}
}

func TestDoctorTailscalePassesWithFunnelCaps(t *testing.T) {
	paths := doctorTestPaths(t, "tailscale")
	t.Setenv(transport.TailscaleBinEnv, fakeTailscale(t, statusWithFunnelCaps(), serveWithFunnel()))

	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "tailscale")
	if check.Status != Pass {
		t.Fatalf("tailscale check = %#v", check)
	}
	if !strings.Contains(check.Detail, "Funnel active") {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func TestDoctorTailscaleWarnsWithoutFunnelCaps(t *testing.T) {
	paths := doctorTestPaths(t, "tailscale")
	t.Setenv(transport.TailscaleBinEnv, fakeTailscale(t, `{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{},"funnel":{}}}}`, serveWithFunnel()))

	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "tailscale")
	if check.Status != Warn {
		t.Fatalf("tailscale check = %#v", check)
	}
	if !strings.Contains(check.Detail, "public port 443") {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func TestDoctorTailscaleSkippedForLAN(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "tailscale")
	if check.Status != Pass {
		t.Fatalf("tailscale check = %#v", check)
	}
	if !strings.Contains(check.Detail, "not selected") {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func doctorTestPaths(t *testing.T, mode string) config.Paths {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg-data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{
		StateDir: dir,
		Socket:   filepath.Join(dir, "onibi.sock"),
		DBFile:   filepath.Join(dir, "onibi.sqlite"),
		EnvFile:  filepath.Join(dir, ".env"),
		LogDir:   filepath.Join(dir, "logs"),
		Config:   filepath.Join(dir, "config.yaml"),
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Transport.Mode = mode
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

func fakeTailscale(t *testing.T, statusJSON, serveJSON string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tailscale")
	body := "#!/bin/sh\ncase \"$*\" in\n" +
		"\"status --json\") cat <<'JSON'\n" + statusJSON + "\nJSON\n;;\n" +
		"\"serve status --json\") cat <<'JSON'\n" + serveJSON + "\nJSON\n;;\n" +
		"*) echo unexpected tailscale args: \"$*\" >&2; exit 2;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func statusWithFunnelCaps() string {
	return `{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{},"funnel":{},"https://tailscale.com/cap/funnel-ports?ports=443":{}}}}`
}

func serveWithFunnel() string {
	return `{"AllowFunnel":{"dev.tail.ts.net:443":true}}`
}

func checkNamed(t *testing.T, report Report, name string) Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("missing check %q in %#v", name, report.Checks)
	return Check{}
}
