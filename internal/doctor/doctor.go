package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/web/transport"
)

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

type Check struct {
	Name         string   `json:"name"`
	Status       Status   `json:"status"`
	Detail       string   `json:"detail"`
	Next         string   `json:"next,omitempty"`
	Fixable      bool     `json:"fixable,omitempty"`
	Impact       string   `json:"impact,omitempty"`
	SafeFix      string   `json:"safe_fix,omitempty"`
	ManualFix    string   `json:"manual_fix,omitempty"`
	FilesTouched []string `json:"files_touched,omitempty"`
	Retry        string   `json:"retry,omitempty"`
	Blocks       []string `json:"blocks,omitempty"`
	Code         string   `json:"code,omitempty"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

func (r Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return true
		}
	}
	return false
}

type Options struct {
	Paths        config.Paths
	Offline      bool
	Service      *service.Manager
	PreferDotenv bool
	Mode         string
	Transport    string
	NotifyBin    string
	AfterUpgrade bool
	GOOS         string
	OSVersion    string
}

func Run(ctx context.Context, opts Options) Report {
	r := runner{ctx: ctx, opts: opts}
	r.run()
	return Report{Checks: r.checks}
}

type runner struct {
	ctx    context.Context
	opts   Options
	checks []Check
}

var ()

func (r *runner) add(name string, st Status, detail string) {
	c := Check{Name: name, Status: st, Detail: detail, Code: codeFor(name)}
	if st != Pass {
		c.Impact = "Related Onibi functionality may be degraded."
		c.SafeFix = "fix the reported condition and rerun onibi doctor"
		c.ManualFix = "inspect local state and config manually"
		c.Retry = "onibi doctor"
		c.Next = c.Retry
	}
	r.checks = append(r.checks, c)
}

func (r *runner) run() {
	switch r.opts.Mode {
	case "", "auto", "preflight", "installed", "ci", "release":
	default:
		r.add("doctor mode", Fail, "invalid mode "+r.opts.Mode)
	}
	r.checkPlatform()
	r.checkStateDir()
	r.checkEnvFile()
	r.checkStoreKey()
	r.checkDB()
	r.checkConfig()
	r.checkGhostty()
	r.checkTransportProvider()
	r.checkLAN()
	r.checkTailscale()
	r.checkWireGuard()
	r.checkZeroTier()
	r.checkLocalCerts()
	r.checkSocket()
	r.checkService()
	r.checkSessionRuntime()
	r.checkHooks()
	if r.opts.AfterUpgrade {
		r.checkAfterUpgradeHooks()
	}
}

func (r *runner) checkPlatform() {
	switch r.hostOS() {
	case "darwin":
		version, err := r.macOSVersion()
		if err != nil {
			r.addPlatform(Fail, "cannot verify macOS version: "+err.Error(), "install macOS 14+ and rerun onibi doctor --release")
			return
		}
		major, err := macOSMajor(version)
		if err != nil {
			r.addPlatform(Fail, "cannot parse macOS version "+strconv.Quote(version), "install macOS 14+ and rerun onibi doctor --release")
			return
		}
		if major < 14 {
			r.addPlatform(Fail, "macOS "+version+" is below the v1 minimum of macOS 14", "upgrade to macOS 14+ and rerun onibi doctor --release")
			return
		}
		r.add("platform", Pass, "macOS "+version+" is a v1 release host (Keychain + launchd)")
	case "linux":
		if r.opts.Mode == "release" {
			r.addPlatform(Fail, "Linux is beta; v1 release approval requires macOS 14+ with Keychain and launchd", "run scripts/macos-release-gate.sh on macOS 14+")
			return
		}
		r.addPlatform(Warn, "Linux is beta; Secret Service and systemd user-service coverage are required before deployment", "run scripts/linux-beta-smoke.sh and do not use Linux for v1 release approval")
	default:
		r.addPlatform(Fail, "unsupported host "+r.hostOS()+"; v1 supports macOS 14+ and Linux beta only", "use macOS 14+ for v1 release approval")
	}
}

func (r *runner) hostOS() string {
	if goos := strings.ToLower(strings.TrimSpace(r.opts.GOOS)); goos != "" {
		return goos
	}
	return runtime.GOOS
}

func (r *runner) macOSVersion() (string, error) {
	if version := strings.TrimSpace(r.opts.OSVersion); version != "" {
		return version, nil
	}
	out, err := exec.CommandContext(r.ctx, "sw_vers", "-productVersion").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func macOSMajor(version string) (int, error) {
	major, _, _ := strings.Cut(strings.TrimSpace(version), ".")
	return strconv.Atoi(major)
}

func (r *runner) addPlatform(st Status, detail, next string) {
	c := Check{Name: "platform", Status: st, Detail: detail, Code: "platform"}
	if st != Pass {
		c.Impact = "This host cannot satisfy the v1 release security and service-runtime requirements."
		c.SafeFix = next
		c.ManualFix = "verify the host OS, credential backend, and user service manually"
		c.Retry = "onibi doctor"
		c.Next = next
		if st == Fail {
			c.Blocks = []string{"release"}
		}
	}
	r.checks = append(r.checks, c)
}

func (r *runner) checkStateDir() {
	if err := r.opts.Paths.EnsureDirs(); err != nil {
		r.add("state dir", Fail, err.Error())
		return
	}
	info, err := os.Stat(r.opts.Paths.StateDir)
	if err != nil {
		r.add("state dir", Fail, err.Error())
		return
	}
	if info.Mode().Perm() != 0o700 {
		r.add("state dir", Warn, fmt.Sprintf("perms %o want 700", info.Mode().Perm()))
		return
	}
	r.add("state dir", Pass, r.opts.Paths.StateDir)
}

func (r *runner) checkEnvFile() {
	info, err := os.Stat(r.opts.Paths.EnvFile)
	if os.IsNotExist(err) {
		r.add(".env fallback", Pass, "not present")
		return
	}
	if err != nil {
		r.add(".env fallback", Warn, err.Error())
		return
	}
	if info.Mode().Perm() != 0o600 {
		r.add(".env fallback", Warn, fmt.Sprintf("perms %o want 600", info.Mode().Perm()))
		return
	}
	r.add(".env fallback", Pass, "present with 0600 perms")
}

func (r *runner) checkStoreKey() {
	key, err := secrets.GetStoreKey(r.ctx)
	if err != nil {
		if errors.Is(err, secrets.ErrStoreKeyNotFound) {
			c := Check{Name: "store key", Status: Fail, Detail: "missing encrypted store key", Code: codeFor("store key")}
			c.Impact = "Encrypted SQLite state cannot be decrypted."
			c.SafeFix = "start Onibi once to create a key, or restore the key from backup"
			c.ManualFix = "verify " + secrets.StoreKeyName + " in the active secret backend or fallback store"
			c.Retry = "onibi doctor"
			c.Next = c.Retry
			r.checks = append(r.checks, c)
			return
		}
		r.add("store key", Fail, err.Error())
		return
	}
	if len(key) != 32 {
		r.add("store key", Fail, fmt.Sprintf("key length %d want 32", len(key)))
		return
	}
	r.add("store key", Pass, "present")
}

func (r *runner) checkDB() {
	db, err := openStoreDB(r.opts.Paths.DBFile)
	if err != nil {
		r.add("sqlite db", Fail, err.Error())
		return
	}
	defer db.Close()
	if err := db.VerifyEncryptedState(r.ctx); err != nil {
		r.add("sqlite db", Fail, "encrypted state decrypt failed: "+err.Error())
		return
	}
	r.add("sqlite db", Pass, r.opts.Paths.DBFile)
}

func (r *runner) checkConfig() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("config", Fail, err.Error())
		return
	}
	mode := r.transportMode(cfg)
	if strings.TrimSpace(mode) == "" {
		r.add("transport", Warn, "not configured")
	} else {
		r.add("transport", Pass, mode)
	}
	r.add("web config", Pass, fmt.Sprintf("listen=%s cert_dir=%s", cfg.Web.ListenAddr, doctorCertDir(r.opts.Paths, cfg)))
}

func (r *runner) checkGhostty() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("ghostty", Warn, err.Error())
		return
	}
	cap := daemon.ProbeGhostty(r.ctx)
	if !cap.Supported {
		r.add("ghostty", Pass, cap.Detail)
		return
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Terminal.Default), "ghostty") && !cap.Installed {
		r.add("ghostty", Warn, "terminal.default=ghostty but Ghostty was not found")
		return
	}
	r.add("ghostty", Pass, cap.Detail)
}

func (r *runner) checkTransportProvider() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("transport provider", Warn, err.Error())
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.transportMode(cfg)))
	switch mode {
	case "lan":
		r.add("transport provider", Pass, "LAN coverage: unit + local integration + manual device")
	case "auto":
		r.add("transport provider", Pass, "Auto coverage: Tailscale -> LAN only")
	case "tailscale":
		r.add("transport provider", Pass, "Tailscale Funnel coverage: unit + fake runner + live opt-in")
	case "tailscale-private":
		r.add("transport provider", Pass, "Tailscale Serve coverage: unit + fake runner")
	case "wireguard":
		r.add("transport provider", Pass, "WireGuard coverage: unit + interface probe + live opt-in")
	case "zerotier":
		r.add("transport provider", Pass, "ZeroTier coverage: unit + CLI probe + live opt-in")
	case "cloudflare-quick":
		r.checkCloudflared("transport provider", "Cloudflare Quick coverage: unit + fake process + live opt-in")
	case "cloudflare-named":
		r.checkCloudflareNamedProvider()
	case "ngrok":
		r.checkNgrokProvider()
	case "telegram":
		r.add("transport provider", Pass, "Telegram coverage: unit + fake API + live opt-in; run onibi telegram status for pairing")
	default:
		r.add("transport provider", Warn, "unknown transport "+mode)
	}
}

func (r *runner) checkCloudflared(name, detail string) {
	if _, err := exec.LookPath(envDefault("ONIBI_CLOUDFLARED_BIN", "cloudflared")); err != nil {
		r.add(name, Warn, "cloudflared binary not found")
		return
	}
	r.add(name, Pass, detail)
}

func (r *runner) checkCloudflareNamedProvider() {
	if missing := missingEnv(transport.CloudflareTunnelNameEnv, transport.CloudflareHostnameEnv); len(missing) > 0 {
		r.add("transport provider", Warn, "Cloudflare Named missing "+strings.Join(missing, ", "))
		return
	}
	if r.opts.Offline {
		r.checkCloudflared("transport provider", "Cloudflare Named env present; tunnel status skipped offline")
		return
	}
	cf := transport.NewCloudflareNamedFromEnv()
	if (strings.TrimSpace(cf.AccountID) != "" || strings.TrimSpace(cf.TunnelID) != "") && strings.TrimSpace(cf.APIToken) == "" && strings.TrimSpace(cf.TunnelToken) == "" {
		r.add("transport provider", Warn, "Cloudflare Named missing API token; run onibi cloudflare setup or set "+transport.CloudflareAPITokenEnv)
		return
	}
	if err := cf.Check(r.ctx); err != nil {
		r.add("transport provider", Warn, err.Error())
		return
	}
	detail := "Cloudflare Named preflight ok: token/tunnel status"
	if strings.TrimSpace(cf.APIToken) != "" {
		detail = "Cloudflare API token present; " + detail
	}
	r.add("transport provider", Pass, detail)
}

func (r *runner) checkNgrokProvider() {
	if _, err := exec.LookPath(envDefault(transport.NgrokBinEnv, "ngrok")); err != nil {
		r.add("transport provider", Warn, "ngrok binary not found")
		return
	}
	ng := transport.NewNgrokFromEnv()
	if err := ng.Check(r.ctx); err != nil {
		r.add("transport provider", Warn, err.Error())
		return
	}
	detail := "ngrok binary ready; exact HTTPS tunnel health is checked through the loopback Agent API during up; relay E2E is required"
	if strings.TrimSpace(ng.Authtoken) != "" {
		detail = "ngrok authtoken present; auth ready; " + detail
	} else {
		detail = "ngrok authtoken not set; ngrok config may provide agent auth; " + detail
	}
	r.add("transport provider", Pass, detail)
}

func (r *runner) transportMode(cfg config.Config) string {
	if v := strings.TrimSpace(r.opts.Transport); v != "" {
		return v
	}
	return cfg.Transport.Mode
}

func missingEnv(names ...string) []string {
	var out []string
	for _, name := range names {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			out = append(out, name)
		}
	}
	return out
}

func envDefault(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func doctorLiveProbe() bool {
	return envBool("ONIBI_DOCTOR_LIVE")
}

func firstCSVEnv(name string) string {
	for _, part := range strings.Split(os.Getenv(name), ",") {
		if s := strings.TrimSpace(part); s != "" {
			return s
		}
	}
	return ""
}

func containsString(vals []string, needle string) bool {
	for _, v := range vals {
		if v == needle {
			return true
		}
	}
	return false
}

func (r *runner) checkLAN() {
	ips := transport.DetectLANIPs()
	if len(ips) == 0 {
		r.add("lan ip", Warn, "no LAN IPv4/IPv6 address detected")
		return
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	r.add("lan ip", Pass, strings.Join(out, ", "))
}

func (r *runner) checkTailscale() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("tailscale", Warn, err.Error())
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.transportMode(cfg)))
	if mode != "tailscale" && mode != "tailscale-private" && mode != "auto" {
		r.add("tailscale", Pass, "not selected (transport="+mode+")")
		return
	}
	bin := transport.TailscaleBin()
	if _, err := exec.LookPath(bin); err != nil {
		r.add("tailscale", Warn, "binary not found in PATH: "+bin)
		return
	}
	ts := transport.NewTailscale()
	if mode == "tailscale-private" {
		ts = transport.NewTailscalePrivate()
	}
	if err := ts.Check(r.ctx); err != nil {
		r.add("tailscale", Warn, err.Error())
		return
	}
	url, urlErr := ts.URL(r.ctx)
	if mode == "tailscale-private" {
		if urlErr != nil {
			r.add("tailscale", Pass, "ready; no active Serve")
			return
		}
		r.add("tailscale", Pass, "ready; Serve active at "+url)
		return
	}
	if urlErr == nil {
		r.add("tailscale", Pass, "ready; Funnel active at "+url)
		return
	}
	r.add("tailscale", Pass, "ready; no active Funnel")
}

func (r *runner) checkWireGuard() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("wireguard", Warn, err.Error())
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.transportMode(cfg)))
	if mode != "wireguard" {
		r.add("wireguard", Pass, "not selected (transport="+mode+")")
		return
	}
	wg := transport.NewWireGuardFromEnv()
	host, err := wg.BindHost(r.ctx)
	if err != nil {
		r.add("wireguard", Warn, err.Error())
		return
	}
	detail := "ready at " + host
	if iface := wg.InterfaceName(); iface != "" {
		detail += " on " + iface
	}
	r.add("wireguard", Pass, detail)
}

func (r *runner) checkZeroTier() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("zerotier", Warn, err.Error())
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.transportMode(cfg)))
	if mode != "zerotier" {
		r.add("zerotier", Pass, "not selected (transport="+mode+")")
		return
	}
	zt := transport.NewZeroTierFromEnv()
	host, err := zt.BindHost(r.ctx)
	if err != nil {
		r.add("zerotier", Warn, err.Error())
		return
	}
	detail := "ready at " + host
	if network := zt.NetworkID(); network != "" {
		detail += " on " + network
	}
	if iface := zt.InterfaceName(); iface != "" {
		detail += " via " + iface
	}
	r.add("zerotier", Pass, detail)
}

func (r *runner) checkLocalCerts() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("local certs", Warn, err.Error())
		return
	}
	paths := web.LocalCertPaths(doctorCertDir(r.opts.Paths, cfg))
	if _, err := os.Stat(paths.MobileConfig); err != nil {
		if os.IsNotExist(err) {
			r.add("local certs", Warn, "not generated; run onibi up")
			return
		}
		r.add("local certs", Warn, err.Error())
		return
	}
	caPEM, err := os.ReadFile(paths.CACert)
	if err != nil {
		r.add("local certs", Warn, err.Error())
		return
	}
	ca, ok := parseDoctorCert(caPEM)
	if !ok || !ca.IsCA || !ca.NotAfter.After(time.Now()) {
		r.add("local certs", Warn, "invalid or expired local CA")
		return
	}
	pair, err := tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil || len(pair.Certificate) == 0 {
		r.add("local certs", Warn, "server certificate missing or invalid")
		return
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.NotAfter.Before(time.Now()) || leaf.CheckSignatureFrom(ca) != nil {
		r.add("local certs", Warn, "server certificate not signed by local CA or expired")
		return
	}
	r.add("local certs", Pass, paths.MobileConfig)
}

func (r *runner) checkSocket() {
	fi, err := os.Stat(r.opts.Paths.Socket)
	if os.IsNotExist(err) {
		r.add("unix socket", Warn, "not running")
		return
	}
	if err != nil {
		r.add("unix socket", Fail, err.Error())
		return
	}
	if fi.Mode().Perm() != 0o600 {
		r.add("unix socket", Warn, fmt.Sprintf("perms %o want 600", fi.Mode().Perm()))
		return
	}
	c, err := net.DialTimeout("unix", r.opts.Paths.Socket, 300*time.Millisecond)
	if err != nil {
		r.add("unix socket", Warn, "not listening: "+err.Error())
		return
	}
	_ = c.Close()
	r.add("unix socket", Pass, "listening")
}

func (r *runner) checkService() {
	m := r.opts.Service
	var err error
	if m == nil {
		m, err = service.NewManager(r.opts.Paths, "")
		if err != nil {
			r.add("service", Warn, err.Error())
			return
		}
	}
	st := m.Status(r.ctx)
	if st.Installed && st.Running {
		r.add("service", Pass, "installed and running")
		return
	}
	if st.Installed {
		r.add("service", Warn, "installed but not running")
		return
	}
	r.add("service", Warn, "not installed")
}

func (r *runner) checkSessionRuntime() {
	if _, err := exec.LookPath("tmux"); err != nil {
		r.add("tmux", Warn, "tmux not found")
	} else {
		r.add("tmux", Pass, "available")
	}
}

func (r *runner) checkHooks() {
	db, err := openStoreDB(r.opts.Paths.DBFile)
	if err != nil {
		r.add("hooks", Warn, err.Error())
		return
	}
	defer db.Close()
	problems := 0
	for _, name := range adapters.Names() {
		a, _ := adapters.Get(name)
		info := a.Status(r.ctx, db)
		if info.Installed && !info.Disabled && !info.Tampered && !info.Outdated {
			continue
		}
		if info.Disabled || info.Tampered || info.Outdated {
			problems++
		}
	}
	if problems > 0 {
		r.add("hooks", Warn, fmt.Sprintf("%d hook issue(s); run onibi hooks --show --all", problems))
		return
	}
	r.add("hooks", Pass, "no managed hook drift detected")
}

func (r *runner) checkAfterUpgradeHooks() {
	removed, err := adapters.CleanupLegacyShellHooks()
	if err != nil {
		r.add("after-upgrade hooks", Warn, "legacy shell hook cleanup failed: "+err.Error())
	} else if removed > 0 {
		r.add("after-upgrade hooks", Pass, fmt.Sprintf("removed %d legacy shell hook block(s)", removed))
	} else {
		r.add("after-upgrade hooks", Pass, "no legacy shell hook blocks found")
	}
	for _, check := range r.checks {
		if (check.Name == "store key" || check.Name == "sqlite db") && check.Status == Fail {
			r.add("after-upgrade durable state", Fail, "encrypted store recovery failed; restore state before continuing")
			return
		}
	}
	r.add("after-upgrade durable state", Pass, "encrypted store key and SQLite state recovered")
}

func codeFor(name string) string {
	repl := strings.NewReplacer(" ", "_", ".", "", ":", "_", "-", "_")
	return repl.Replace(strings.ToLower(name))
}

func doctorCertDir(paths config.Paths, cfg config.Config) string {
	if cfg.Web.CertDir != "" {
		return cfg.Web.CertDir
	}
	return filepath.Join(paths.StateDir, "web")
}

func parseDoctorCert(certPEM []byte) (*x509.Certificate, bool) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	return cert, err == nil
}
