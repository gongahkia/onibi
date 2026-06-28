package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/store"
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
	case "", "auto", "preflight", "installed", "ci":
	default:
		r.add("doctor mode", Fail, "invalid mode "+r.opts.Mode)
	}
	r.checkStateDir()
	r.checkEnvFile()
	r.checkDB()
	r.checkConfig()
	r.checkTransportProvider()
	r.checkLAN()
	r.checkTailscale()
	r.checkLocalCerts()
	r.checkSocket()
	r.checkService()
	r.checkSessionRuntime()
	r.checkHooks()
	if r.opts.AfterUpgrade {
		r.checkAfterUpgradeHooks()
	}
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

func (r *runner) checkDB() {
	db, err := store.Open(r.opts.Paths.DBFile)
	if err != nil {
		r.add("sqlite db", Fail, err.Error())
		return
	}
	defer db.Close()
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
		r.add("transport provider", Pass, "Tailscale coverage: unit + fake runner + live opt-in")
	case "cloudflare-quick":
		r.checkCloudflared("transport provider", "Cloudflare Quick coverage: unit + fake process + live opt-in")
	case "cloudflare-named":
		if missing := missingEnv("ONIBI_CLOUDFLARE_TUNNEL_NAME", "ONIBI_CLOUDFLARE_HOSTNAME"); len(missing) > 0 {
			r.add("transport provider", Warn, "Cloudflare Named missing "+strings.Join(missing, ", "))
			return
		}
		r.checkCloudflared("transport provider", "Cloudflare Named coverage: unit + fake process + live opt-in")
	case "ngrok":
		if _, err := exec.LookPath(envDefault("ONIBI_NGROK_BIN", "ngrok")); err != nil {
			r.add("transport provider", Warn, "ngrok binary not found")
			return
		}
		r.add("transport provider", Pass, "ngrok coverage: unit + fake agent API + live opt-in")
	case "telegram":
		r.add("transport provider", Pass, "Telegram coverage: unit + fake API + live opt-in; run onibi telegram status for pairing")
	case "matrix":
		if missing := missingEnv("ONIBI_MATRIX_HOMESERVER", "ONIBI_MATRIX_ACCESS_TOKEN", "ONIBI_MATRIX_ROOM_ID"); len(missing) > 0 {
			r.add("transport provider", Warn, "Matrix missing "+strings.Join(missing, ", "))
			return
		}
		r.add("transport provider", Pass, "Matrix coverage: unit + daemon conformance + live opt-in; encrypted rooms blocked by default")
	case "slack":
		if missing := missingEnv("ONIBI_SLACK_APP_TOKEN", "ONIBI_SLACK_BOT_TOKEN"); len(missing) > 0 {
			r.add("transport provider", Warn, "Slack missing "+strings.Join(missing, ", "))
			return
		}
		r.add("transport provider", Pass, "Slack coverage: unit + fake socket + live opt-in")
	case "discord":
		if missing := missingEnv("ONIBI_DISCORD_TOKEN"); len(missing) > 0 {
			r.add("transport provider", Warn, "Discord missing "+strings.Join(missing, ", "))
			return
		}
		r.add("transport provider", Pass, "Discord coverage: unit + fake gateway + live opt-in")
	case "pushover":
		if missing := missingEnv("ONIBI_PUSHOVER_TOKEN", "ONIBI_PUSHOVER_USER_KEY"); len(missing) > 0 {
			r.add("transport provider", Warn, "Pushover missing "+strings.Join(missing, ", "))
			return
		}
		r.add("transport provider", Pass, "Pushover coverage: unit + receipt audit + live opt-in")
	case "ntfy":
		topic := strings.TrimSpace(os.Getenv("ONIBI_NTFY_TOPIC"))
		if topic == "" {
			r.add("transport provider", Warn, "ntfy missing ONIBI_NTFY_TOPIC")
			return
		}
		if err := ntfy.ValidateTopicSecret(topic); err != nil {
			r.add("transport provider", Warn, "ntfy topic weak: "+err.Error())
			return
		}
		r.add("transport provider", Pass, "ntfy coverage: unit + fake WS + live opt-in")
	case "gotify":
		if missing := missingEnv("ONIBI_GOTIFY_URL", "ONIBI_GOTIFY_APP_TOKEN"); len(missing) > 0 {
			r.add("transport provider", Warn, "Gotify missing "+strings.Join(missing, ", "))
			return
		}
		if !r.opts.Offline {
			ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
			defer cancel()
			if err := gotify.New(os.Getenv("ONIBI_GOTIFY_URL"), os.Getenv("ONIBI_GOTIFY_APP_TOKEN"), os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN")).Validate(ctx); err != nil {
				r.add("transport provider", Warn, "Gotify validation failed: "+err.Error())
				return
			}
		}
		r.add("transport provider", Pass, "Gotify coverage: unit + REST/WS fake + live opt-in")
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
	if mode != "tailscale" && mode != "auto" {
		r.add("tailscale", Pass, "not selected (transport="+mode+")")
		return
	}
	bin := transport.TailscaleBin()
	if _, err := exec.LookPath(bin); err != nil {
		r.add("tailscale", Warn, "binary not found in PATH: "+bin)
		return
	}
	ts := transport.NewTailscale()
	if err := ts.Check(r.ctx); err != nil {
		r.add("tailscale", Warn, err.Error())
		return
	}
	if url, err := ts.URL(r.ctx); err == nil {
		r.add("tailscale", Pass, "ready; Funnel active at "+url)
		return
	}
	r.add("tailscale", Pass, "ready; no active Funnel")
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
	db, err := store.Open(r.opts.Paths.DBFile)
	if err != nil {
		r.add("hooks", Warn, err.Error())
		return
	}
	defer db.Close()
	problems := 0
	for _, name := range adapters.Names() {
		a, _ := adapters.Get(name)
		info := a.Status(r.ctx, db)
		if info.Installed && !info.Tampered && !info.Outdated {
			continue
		}
		if info.Tampered || info.Outdated {
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
	r.add("after-upgrade hooks", Pass, "no legacy transport checks")
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
