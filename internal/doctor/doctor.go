package doctor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
)

// Status is a check result class.
type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

// Check is one doctor line.
type Check struct {
	Name    string
	Status  Status
	Detail  string
	Next    string
	Fixable bool
}

// Report is the full doctor result.
type Report struct {
	Checks []Check
}

// Failed reports whether any check failed.
func (r Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return true
		}
	}
	return false
}

// Options configures Run.
type Options struct {
	Paths        config.Paths
	Offline      bool
	Service      *service.Manager
	PreferDotenv bool
	Mode         string
	NotifyBin    string
}

// Run performs local integrity checks and optional live Telegram checks.
func Run(ctx context.Context, opts Options) Report {
	d := runner{ctx: ctx, opts: opts}
	d.run()
	return Report{Checks: d.checks}
}

type runner struct {
	ctx      context.Context
	opts     Options
	checks   []Check
	db       *store.DB
	sec      *secrets.Store
	token    string
	hasOwner bool
}

func (r *runner) add(name string, st Status, detail string) {
	next, fixable := nextAction(name, detail, st)
	r.checks = append(r.checks, Check{Name: name, Status: st, Detail: detail, Next: next, Fixable: fixable})
}

func (r *runner) run() {
	r.normalizeMode()
	r.checkStateDir()
	r.checkEnvFile()
	r.openDB()
	defer func() {
		if r.db != nil {
			_ = r.db.Close()
		}
	}()
	r.openSecrets()
	r.checkToken()
	r.checkOwner()
	r.checkSocket()
	r.checkService()
	r.checkHooks()
	r.checkTOTP()
	if !r.opts.Offline {
		r.checkTelegram()
	}
}

func nextAction(name, detail string, st Status) (string, bool) {
	if st == Pass {
		return "", false
	}
	switch {
	case name == "state dir":
		return "onibi doctor --fix", true
	case name == ".env fallback" && strings.Contains(detail, "perms"):
		return "onibi doctor --fix", true
	case name == ".env fallback":
		return "keep OS keychain preferred; use onibi rotate-token after keychain is available", false
	case name == "sqlite db":
		return "onibi setup --complete", false
	case name == "secrets backend":
		return "install OS keyring support, then run onibi rotate-token", false
	case name == "bot token":
		return "onibi setup --complete", false
	case name == "owner chat_id":
		return "onibi setup --complete", false
	case name == "unix socket" && strings.Contains(detail, "not listening"):
		return "onibi doctor --fix", true
	case name == "unix socket":
		return "onibi install-service or onibi run", false
	case name == "service":
		return "onibi install-service", true
	case name == "hooks":
		return "onibi install-hooks --interactive", false
	case strings.HasPrefix(name, "hook ") && strings.Contains(detail, "hash missing"):
		return "onibi doctor --fix", true
	case strings.HasPrefix(name, "hook ") && strings.Contains(detail, "tampered"):
		return "review hook file, then run onibi install-hooks --interactive", false
	case strings.HasPrefix(name, "hook ") && strings.Contains(detail, "outdated"):
		return "onibi doctor --fix", true
	case strings.HasPrefix(name, "hook "):
		return "onibi install-hooks --interactive", false
	case name == "totp":
		return "onibi setup --enable-totp", false
	case name == "telegram 2fa ack":
		return "enable Telegram 2-step verification, then rerun setup when rotating owner", false
	case name == "telegram reachability":
		return "see docs/troubleshooting.md#no-telegram-updates", false
	case name == "telegram webhook":
		return "restart onibi; startup deletes webhooks, then run onibi doctor", false
	case name == "bot identity":
		return "onibi rotate-token", false
	case name == "doctor mode":
		return "onibi doctor --mode auto", false
	}
	return "", false
}

func (r *runner) normalizeMode() {
	switch r.opts.Mode {
	case "", "auto", "preflight", "installed", "ci":
		if r.opts.Mode == "" {
			r.opts.Mode = "auto"
		}
	default:
		r.add("doctor mode", Fail, "invalid mode "+r.opts.Mode)
		r.opts.Mode = "auto"
	}
	if r.opts.Mode == "ci" {
		r.opts.Offline = true
	}
}

func (r *runner) installedStrict() bool {
	if r.opts.Mode == "installed" {
		return true
	}
	return r.opts.Mode == "auto" && r.token != "" && r.hasOwner
}

func (r *runner) missingStatus() Status {
	if r.opts.Mode == "installed" {
		return Fail
	}
	return Warn
}

func (r *runner) checkStateDir() {
	fi, err := os.Stat(r.opts.Paths.StateDir)
	if err != nil {
		r.add("state dir", r.missingStatus(), err.Error())
		return
	}
	if !fi.IsDir() {
		r.add("state dir", Fail, "not a directory")
		return
	}
	if p := fi.Mode().Perm(); p != 0o700 {
		r.add("state dir", Fail, fmt.Sprintf("perms %#o want 0700", p))
		return
	}
	r.add("state dir", Pass, r.opts.Paths.StateDir)
}

func (r *runner) checkEnvFile() {
	fi, err := os.Stat(r.opts.Paths.EnvFile)
	if errors.Is(err, os.ErrNotExist) {
		r.add(".env fallback", Pass, "not present")
		return
	}
	if err != nil {
		r.add(".env fallback", Fail, err.Error())
		return
	}
	if p := fi.Mode().Perm(); p != 0o600 {
		r.add(".env fallback", Fail, fmt.Sprintf("perms %#o want 0600", p))
		return
	}
	r.add(".env fallback", Warn, "present; OS keystore preferred")
}

func (r *runner) openDB() {
	if _, err := os.Stat(r.opts.Paths.DBFile); err != nil {
		r.add("sqlite db", r.missingStatus(), err.Error())
		return
	}
	db, err := store.Open(r.opts.Paths.DBFile)
	if err != nil {
		r.add("sqlite db", Fail, err.Error())
		return
	}
	r.db = db
	r.add("sqlite db", Pass, r.opts.Paths.DBFile)
}

func (r *runner) openSecrets() {
	sec, err := secrets.Open(secrets.Options{
		EnvFallbackPath: r.opts.Paths.EnvFile,
		PreferDotenv:    r.opts.PreferDotenv,
	})
	if err != nil {
		r.add("secrets backend", Fail, err.Error())
		return
	}
	r.sec = sec
	if sec.Backend() == secrets.BackendDotenv {
		r.add("secrets backend", Warn, "dotenv fallback")
		return
	}
	r.add("secrets backend", Pass, string(sec.Backend()))
}

func (r *runner) checkToken() {
	if r.sec == nil {
		return
	}
	token, ok, err := getSecret(r.ctx, r.sec, secrets.KeyBotToken)
	if err != nil {
		r.add("bot token", Fail, err.Error())
		return
	}
	if !ok || token == "" {
		r.add("bot token", r.missingStatus(), "missing")
		return
	}
	r.token = token
	r.add("bot token", Pass, "present")
}

func (r *runner) checkOwner() {
	if r.db == nil {
		return
	}
	v, ok, err := r.db.KVGetString(r.ctx, auth.KVKeyOwnerID)
	if err != nil {
		r.add("owner chat_id", Fail, err.Error())
		return
	}
	if !ok || v == "" {
		r.add("owner chat_id", r.missingStatus(), "missing")
		return
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id == 0 {
		r.add("owner chat_id", Fail, "invalid")
		return
	}
	r.hasOwner = true
	r.add("owner chat_id", Pass, v)
}

func (r *runner) checkSocket() {
	fi, err := os.Stat(r.opts.Paths.Socket)
	if errors.Is(err, os.ErrNotExist) {
		st := Warn
		if r.installedStrict() {
			st = Fail
		}
		r.add("unix socket", st, "not present; daemon not running")
		return
	}
	if err != nil {
		r.add("unix socket", Fail, err.Error())
		return
	}
	if p := fi.Mode().Perm(); p != 0o600 {
		r.add("unix socket", Fail, fmt.Sprintf("perms %#o want 0600", p))
		return
	}
	c, err := net.DialTimeout("unix", r.opts.Paths.Socket, 500*time.Millisecond)
	if err != nil {
		r.add("unix socket", Fail, "not listening: "+err.Error())
		return
	}
	_ = c.Close()
	r.add("unix socket", Pass, "listening")
}

func (r *runner) checkService() {
	m := r.opts.Service
	if m == nil {
		var err error
		m, err = service.NewManager(r.opts.Paths, "")
		if err != nil {
			r.add("service", Fail, err.Error())
			return
		}
	}
	st := m.Status(r.ctx)
	if !st.Installed {
		status := Warn
		if r.installedStrict() {
			status = Fail
		}
		r.add("service", status, "not installed")
		return
	}
	if !st.Running {
		status := Warn
		if r.installedStrict() {
			status = Fail
		}
		r.add("service", status, "installed but not running: "+st.Detail)
		return
	}
	r.add("service", Pass, st.Path)
}

func (r *runner) checkHooks() {
	if r.db == nil {
		return
	}
	rows, err := r.db.SQL().QueryContext(r.ctx, `SELECT agent, path FROM hooks ORDER BY agent, path`)
	if err != nil {
		r.add("hooks", Fail, err.Error())
		return
	}
	var records []struct {
		agent string
		path  string
	}
	seen := map[string]bool{}
	for rows.Next() {
		var agent, path string
		if err := rows.Scan(&agent, &path); err != nil {
			_ = rows.Close()
			r.add("hooks", Fail, err.Error())
			return
		}
		records = append(records, struct {
			agent string
			path  string
		}{agent: agent, path: path})
		seen[agent] = true
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		r.add("hooks", Fail, err.Error())
		return
	}
	if err := rows.Close(); err != nil {
		r.add("hooks", Fail, err.Error())
		return
	}
	n := 0
	for _, rec := range records {
		n++
		agent, path := rec.agent, rec.path
		if strings.HasPrefix(agent, "shell:") {
			name := strings.TrimPrefix(agent, "shell:")
			r.addRecordedHookStatus(agent, path, adapters.ShellStatus(r.ctx, r.db, name))
			continue
		}
		if a, ok := adapters.Get(agent); ok {
			r.addRecordedHookStatus(agent, path, a.Status(r.ctx, r.db))
			continue
		}
		r.add("hook "+agent, Warn, "no verifier implemented for "+path)
	}
	for _, name := range adapters.Names() {
		if seen[name] {
			continue
		}
		info := adapters.Registry()[name].Status(r.ctx, r.db)
		if info.Installed {
			n++
			st := Warn
			if info.Tampered {
				st = Fail
			}
			r.add("hook "+name, st, info.Message)
		}
	}
	for _, name := range adapters.ShellNames() {
		agent := "shell:" + name
		if seen[agent] {
			continue
		}
		info := adapters.ShellStatus(r.ctx, r.db, name)
		if info.Installed {
			n++
			st := Warn
			if info.Tampered {
				st = Fail
			}
			r.add("hook "+agent, st, info.Message)
		}
	}
	if n == 0 {
		r.add("hooks", Warn, "none installed")
	}
	if rows, err := r.db.PromptList(r.ctx, "", false, 1000); err == nil {
		r.add("prompt queue", Pass, fmt.Sprintf("%d queued", len(rows)))
	}
}

func (r *runner) addRecordedHookStatus(agent, path string, info common.Info) {
	detail := info.Message
	if detail == "" {
		detail = path
	}
	switch {
	case !info.Installed || !info.Managed:
		r.add("hook "+agent, Fail, detail)
	case info.Tampered:
		r.add("hook "+agent, Fail, detail)
	case !info.HashRecorded:
		r.add("hook "+agent, Fail, detail)
	case info.Outdated:
		r.add("hook "+agent, Warn, detail)
	default:
		if info.InstallPath != "" {
			detail = info.InstallPath
		}
		r.add("hook "+agent, Pass, detail)
	}
}

func (r *runner) checkTOTP() {
	if r.sec == nil {
		return
	}
	_, ok, err := getSecret(r.ctx, r.sec, secrets.KeyTOTPSecret)
	if err != nil {
		r.add("totp", Fail, err.Error())
		return
	}
	if ok {
		r.add("totp", Pass, "enabled")
	} else {
		r.add("totp", Warn, "optional second factor not enabled")
	}
	if r.db != nil {
		v, ok, _ := r.db.KVGetString(r.ctx, "tg_2fa_ack")
		switch {
		case !ok:
			r.add("telegram 2fa ack", Warn, "not acknowledged")
		case v == "enabled":
			r.add("telegram 2fa ack", Pass, v)
		case v == "timeout":
			r.add("telegram 2fa ack", Warn, "timeout (timed out during setup)")
		default:
			r.add("telegram 2fa ack", Warn, v)
		}
	}
}

const secretLookupTimeout = 30 * time.Second

type secretResult struct {
	value string
	ok    bool
	err   error
}

func getSecret(ctx context.Context, sec *secrets.Store, key string) (string, bool, error) {
	if sec.Backend() == secrets.BackendDotenv {
		return sec.Get(key)
	}
	ctx, cancel := context.WithTimeout(ctx, secretLookupTimeout)
	defer cancel()
	ch := make(chan secretResult, 1)
	go func() {
		value, ok, err := sec.Get(key)
		ch <- secretResult{value: value, ok: ok, err: err}
	}()
	select {
	case res := <-ch:
		return res.value, res.ok, res.err
	case <-ctx.Done():
		return "", false, fmt.Errorf("secret %s lookup timeout: %w", key, ctx.Err())
	}
}

func (r *runner) checkTelegram() {
	if r.token == "" {
		return
	}
	res, err := telegram.ProbeToken(r.ctx, r.token, false)
	if err != nil {
		r.add("telegram reachability", Fail, err.Error())
		return
	}
	r.add("telegram getMe", Pass, res.Self.Username)
	if res.WebhookURL != "" {
		r.add("telegram webhook", Warn, res.WebhookDetail)
	}
	if r.db != nil {
		want, ok, _ := r.db.KVGetString(r.ctx, auth.KVKeyBotID)
		got := fmt.Sprintf("%d", res.Self.ID)
		if ok && want != "" && want != got {
			r.add("bot identity", Fail, "stored "+want+" got "+got)
		} else {
			r.add("bot identity", Pass, got)
		}
	}
	if res.GetUpdatesOK {
		r.add("telegram getUpdates", Pass, res.GetUpdatesDetail)
	} else if res.GetUpdatesDetail != "" {
		r.add("telegram getUpdates", Warn, res.GetUpdatesDetail)
	}
}
