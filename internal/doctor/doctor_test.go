package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/web/transport"
)

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
