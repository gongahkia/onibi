package doctor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/web"
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

func TestDoctorPlatformPolicy(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		goos    string
		version string
		status  Status
		detail  string
	}{
		{name: "macOS release host", mode: "release", goos: "darwin", version: "14.5.1", status: Pass, detail: "v1 release host"},
		{name: "old macOS", mode: "release", goos: "darwin", version: "13.6.7", status: Fail, detail: "below the v1 minimum"},
		{name: "Linux beta", mode: "preflight", goos: "linux", status: Warn, detail: "Linux is beta"},
		{name: "Linux release", mode: "release", goos: "linux", status: Fail, detail: "requires macOS 14+"},
		{name: "unsupported host", mode: "preflight", goos: "freebsd", status: Fail, detail: "unsupported host freebsd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Run(context.Background(), Options{Paths: doctorTestPaths(t, "lan"), Mode: tt.mode, GOOS: tt.goos, OSVersion: tt.version})
			check := checkNamed(t, report, "platform")
			if check.Status != tt.status || !strings.Contains(check.Detail, tt.detail) {
				t.Fatalf("platform check = %#v", check)
			}
			if tt.status == Fail && !slices.Contains(check.Blocks, "release") {
				t.Fatalf("platform blocks = %#v", check.Blocks)
			}
		})
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

func TestDoctorAfterUpgradeReportsDurableStateRecovery(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	key, err := secrets.GetOrCreateStoreKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	report := Run(context.Background(), Options{Paths: paths, AfterUpgrade: true})
	if check := checkNamed(t, report, "after-upgrade durable state"); check.Status != Pass {
		t.Fatalf("after-upgrade durable state = %#v", check)
	}
}

func TestDoctorAfterUpgradeRemovesLegacyShellHook(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	zshrc := filepath.Join(os.Getenv("HOME"), ".zshrc")
	block := "# >>> onibi managed shell hook\nlegacy\n# <<< onibi managed shell hook\n"
	if err := os.WriteFile(zshrc, []byte("before\n"+block+"after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), Options{Paths: paths, AfterUpgrade: true})
	if check := checkNamed(t, report, "after-upgrade hooks"); check.Status != Pass || !strings.Contains(check.Detail, "removed 1") {
		t.Fatalf("after-upgrade hooks = %#v", check)
	}
	if got, err := os.ReadFile(zshrc); err != nil || string(got) != "before\nafter\n" {
		t.Fatalf("zshrc = %q err=%v", got, err)
	}
}

func TestDoctorAfterUpgradeFailsWhenDurableStateCannotRecover(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	report := Run(context.Background(), Options{Paths: paths, AfterUpgrade: true})
	if check := checkNamed(t, report, "after-upgrade durable state"); check.Status != Fail {
		t.Fatalf("after-upgrade durable state = %#v", check)
	}
}

func TestDoctorPushPassesWithMatchingVAPIDState(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	key, err := secrets.GetOrCreateStoreKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := web.EnsureVAPIDKeys(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.PutPushSubscription(context.Background(), "https://push.example.invalid/sub/1", "p-key", "a-key", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	report := Push(context.Background(), Options{Paths: paths})
	if report.Failed() {
		t.Fatalf("push doctor failed: %#v", report.Checks)
	}
	if check := checkNamed(t, report, "push subscriptions"); check.Status != Pass || !strings.Contains(check.Detail, "1 subscription") {
		t.Fatalf("subscriptions check = %#v", check)
	}
}

func TestDoctorPushFailsMissingVAPIDKey(t *testing.T) {
	paths := doctorTestPaths(t, "lan")
	key, err := secrets.GetOrCreateStoreKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(paths.DBFile, store.WithStoreKey(key))
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	report := Push(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "push vapid key")
	if check.Status != Fail || !strings.Contains(check.Detail, web.PushVAPIDSecretName) {
		t.Fatalf("push vapid check = %#v", check)
	}
}

func TestDoctorTailscalePrivatePassesWithoutPublicCaps(t *testing.T) {
	paths := doctorTestPaths(t, "tailscale-private")
	t.Setenv(transport.TailscaleBinEnv, fakeTailscale(t, `{"BackendState":"Running","Self":{"DNSName":"dev.tail.ts.net.","CapMap":{"https":{}}}}`, `{"Web":{"dev.tail.ts.net:443":{}}}`))

	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "tailscale")
	if check.Status != Pass || !strings.Contains(check.Detail, "Serve active") {
		t.Fatalf("tailscale check = %#v", check)
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

func TestDoctorZeroTierPassesWithJoinedNetwork(t *testing.T) {
	paths := doctorTestPaths(t, "zerotier")
	t.Setenv(transport.ZeroTierBinEnv, fakeZeroTier(t, `[{"id":"8056c2e21c000001","name":"dev","status":"OK","assignedAddresses":["10.147.20.4/24"]}]`))

	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "zerotier")
	if check.Status != Pass {
		t.Fatalf("zerotier check = %#v", check)
	}
	if !strings.Contains(check.Detail, "10.147.20.4") || !strings.Contains(check.Detail, "8056c2e21c000001") {
		t.Fatalf("detail = %q", check.Detail)
	}
}

func TestDoctorNgrokPassesWithToken(t *testing.T) {
	paths := doctorTestPaths(t, "ngrok")
	t.Setenv(transport.NgrokBinEnv, fakeExecutable(t, "ngrok"))
	t.Setenv(transport.NgrokDomainEnv, "demo.ngrok-free.app")
	t.Setenv(transport.NgrokAuthtokenEnv, "ngrok-token-1234567890")
	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "authtoken present") || !strings.Contains(check.Detail, "relay E2E is required") {
		t.Fatalf("ngrok check = %#v", check)
	}
}

func TestDoctorNgrokWarnsDomainWithoutToken(t *testing.T) {
	paths := doctorTestPaths(t, "ngrok")
	t.Setenv(transport.NgrokBinEnv, fakeExecutable(t, "ngrok"))
	t.Setenv(transport.NgrokDomainEnv, "demo.ngrok-free.app")
	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "transport provider")
	if check.Status != Warn || !strings.Contains(check.Detail, transport.NgrokAuthtokenEnv) {
		t.Fatalf("ngrok check = %#v", check)
	}
}

func TestDoctorNgrokExplainsMissingOnibiToken(t *testing.T) {
	paths := doctorTestPaths(t, "ngrok")
	t.Setenv(transport.NgrokBinEnv, fakeExecutable(t, "ngrok"))
	t.Setenv(transport.NgrokAuthtokenEnv, "")
	report := Run(context.Background(), Options{Paths: paths})
	check := checkNamed(t, report, "transport provider")
	if check.Status != Pass || !strings.Contains(check.Detail, "authtoken not set") || !strings.Contains(check.Detail, "config may provide agent auth") {
		t.Fatalf("ngrok check = %#v", check)
	}
}

func doctorTestPaths(t *testing.T, mode string) config.Paths {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg-data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
	t.Setenv("ONIBI_STORE_KEY_BACKEND", "dotenv")
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

func fakeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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

func fakeZeroTier(t *testing.T, networksJSON string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zerotier-cli")
	body := "#!/bin/sh\ncase \"$*\" in\n" +
		"\"info\") echo '200 info deadbeef 1.14.2 ONLINE'\n;;\n" +
		"\"-j listnetworks\") cat <<'JSON'\n" + networksJSON + "\nJSON\n;;\n" +
		"*) echo unexpected zerotier args: \"$*\" >&2; exit 2;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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
