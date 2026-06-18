package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/adapters/common"
	"github.com/gongahkia/onibi/internal/auth"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/miniappurl"
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

// Report is the full doctor result.
type Report struct {
	Checks []Check `json:"checks"`
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
	AfterUpgrade bool
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

var telegramProbeToken = telegram.ProbeToken

func (r *runner) add(name string, st Status, detail string) {
	spec := repairSpecFor(name, detail, st)
	r.checks = append(r.checks, Check{
		Name:         name,
		Status:       st,
		Detail:       detail,
		Next:         spec.Next(),
		Fixable:      spec.Fixable,
		Impact:       spec.Impact,
		SafeFix:      spec.SafeFix,
		ManualFix:    spec.ManualFix,
		FilesTouched: spec.FilesTouched,
		Retry:        spec.Retry,
		Blocks:       spec.Blocks,
		Code:         spec.Code,
	})
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
	r.checkEncryptedReadiness()
	r.checkSocket()
	r.checkService()
	r.checkSessionRuntime()
	r.checkHooks()
	if r.opts.AfterUpgrade {
		r.checkAfterUpgradeHooks()
	}
	r.checkTOTP()
	if !r.opts.Offline {
		r.checkTelegram()
	}
}

type repairSpec struct {
	Code         string
	Impact       string
	SafeFix      string
	ManualFix    string
	FilesTouched []string
	Retry        string
	Blocks       []string
	Fixable      bool
}

func (s repairSpec) Next() string {
	if s.Retry != "" {
		return s.Retry
	}
	return s.SafeFix
}

type repairRule struct {
	match func(name, detail string) bool
	spec  func(name, detail string) repairSpec
}

func nextAction(name, detail string, st Status) (string, bool) {
	spec := repairSpecFor(name, detail, st)
	return spec.Next(), spec.Fixable
}

func repairSpecFor(name, detail string, st Status) repairSpec {
	if st == Pass {
		return repairSpec{}
	}
	for _, rule := range repairRules {
		if rule.match(name, detail) {
			return completeRepairSpec(rule.spec(name, detail), name)
		}
	}
	return completeRepairSpec(repairSpec{
		Code:      codeFor(name),
		Impact:    "This check did not pass; related Onibi functionality may be degraded.",
		SafeFix:   "rerun onibi doctor with more context",
		ManualFix: "inspect the reported detail and relevant config manually",
		Retry:     "onibi doctor",
	}, name)
}

func completeRepairSpec(spec repairSpec, name string) repairSpec {
	if spec.Code == "" {
		spec.Code = codeFor(name)
	}
	if spec.Impact == "" {
		spec.Impact = "This check can degrade related Onibi functionality."
	}
	if spec.SafeFix == "" {
		spec.SafeFix = "rerun onibi doctor after fixing the reported condition"
	}
	if spec.ManualFix == "" {
		spec.ManualFix = "inspect the reported detail manually"
	}
	if spec.FilesTouched == nil {
		spec.FilesTouched = []string{}
	}
	if spec.Blocks == nil {
		spec.Blocks = []string{}
	}
	return spec
}

var repairRules = []repairRule{
	{matchName("state dir"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Onibi cannot safely store state, sockets, logs, and the SQLite DB.", SafeFix: "fix state directory permissions", ManualFix: "chmod 0700 the Onibi state directory or recreate it", FilesTouched: []string{"state dir"}, Retry: "onibi doctor --fix", Blocks: []string{"daemon startup", "setup", "state writes"}, Fixable: true}
	}},
	{matchName(".env fallback"), func(_, detail string) repairSpec {
		if strings.Contains(detail, "perms") {
			return repairSpec{Impact: "The dotenv fallback may expose secrets if permissions are loose.", SafeFix: "chmod .env to 0600", ManualFix: "remove the dotenv fallback or set strict owner-only permissions", FilesTouched: []string{".env"}, Retry: "onibi doctor --fix", Blocks: []string{"dotenv fallback secrets"}, Fixable: true}
		}
		return repairSpec{Impact: "Bot token storage is using a file fallback instead of the OS keystore.", SafeFix: "keep OS keychain preferred; retry after keychain is available", ManualFix: "move the token back to keychain with onibi rotate-token", FilesTouched: []string{}, Retry: "onibi rotate-token"}
	}},
	{matchName("sqlite db"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Onibi cannot read durable approvals, sessions, prompts, owners, hooks, or audit state.", SafeFix: "rerun setup to create or migrate the database", ManualFix: "restore the SQLite DB from backup or move the corrupt file aside", FilesTouched: []string{"onibi.sqlite"}, Retry: "onibi setup --complete", Blocks: []string{"daemon state", "approvals", "sessions", "hook verification"}}
	}},
	{matchName("secrets backend"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Onibi cannot read or store the bot token/TOTP secrets.", SafeFix: "unlock/login to the OS keychain, then retry", ManualFix: "repair OS keyring support or intentionally use dotenv fallback", FilesTouched: []string{}, Retry: "onibi rotate-token", Blocks: []string{"Telegram startup", "approvals", "notifications"}}
	}},
	{matchName("bot token"), botTokenRepairSpec},
	{matchName("owner chat_id"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Onibi cannot authorize Telegram control without an owner chat id.", SafeFix: "complete pairing again", ManualFix: "set the owner only through the pairing flow; do not guess chat ids", FilesTouched: []string{"onibi.sqlite"}, Retry: "onibi setup --complete", Blocks: []string{"Telegram commands", "approvals", "notifications"}}
	}},
	{matchName("unix socket"), unixSocketRepairSpec},
	{matchName("service"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "The daemon may not start automatically, so Telegram controls may be offline.", SafeFix: "install/start the user service", ManualFix: "run onibi run manually for foreground use", FilesTouched: []string{"LaunchAgent or systemd user unit"}, Retry: "onibi install-service", Blocks: []string{"background daemon", "Telegram controls"}, Fixable: true}
	}},
	{matchName("tmux"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Headless/visible session switching requires tmux.", SafeFix: "install tmux", ManualFix: "install tmux with your OS package manager", FilesTouched: []string{}, Retry: "brew install tmux", Blocks: []string{"headless sessions", "visible session switching"}}
	}},
	{matchName("terminal launcher"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Visible session windows may not open automatically.", SafeFix: "set terminal.default=none or install Ghostty/iTerm2", ManualFix: "open tmux attach commands manually", FilesTouched: []string{"config.yaml"}, Retry: "set terminal.default=none or install Ghostty/iTerm2", Blocks: []string{"visible terminal launch"}}
	}},
	{matchName("hooks"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Agent hooks are not installed, so approvals/turn notifications may not work.", SafeFix: "install supported hooks interactively", ManualFix: "inspect and install per-agent hooks manually", FilesTouched: []string{"provider hook configs"}, Retry: "onibi install-hooks --interactive", Blocks: []string{"agent approvals", "turn notifications"}}
	}},
	{matchPrefix("hook "), hookRepairSpec},
	{matchPrefix("after-upgrade hook "), func(_, _ string) repairSpec {
		return repairSpec{Impact: "A provider may reject stale or schema-risk hook config on startup.", SafeFix: "reinstall hooks with the current Onibi binary", ManualFix: "remove legacy Onibi metadata fields and stale commands by hand", FilesTouched: []string{"provider hook configs"}, Retry: "onibi install-hooks --interactive", Blocks: []string{"provider startup", "hook loading"}}
	}},
	{matchName("totp"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Telegram control has no optional second factor.", SafeFix: "enable TOTP if desired", ManualFix: "leave disabled only if Telegram owner auth is sufficient for your risk model", FilesTouched: []string{"secret store"}, Retry: "onibi setup --enable-totp"}
	}},
	{matchName("telegram 2fa ack"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Owner account hardening has not been acknowledged.", SafeFix: "enable Telegram 2-step verification, then rerun setup when rotating owner", ManualFix: "document the accepted risk if you intentionally skip Telegram 2FA", FilesTouched: []string{"onibi.sqlite"}, Retry: "onibi setup --complete"}
	}},
	{matchName("telegram reachability"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Onibi cannot reach Telegram, so bot commands, approvals, and notifications fail.", SafeFix: "check network/proxy/token, then retry", ManualFix: "see docs/troubleshooting.md#no-telegram-updates", FilesTouched: []string{}, Retry: "onibi doctor", Blocks: []string{"Telegram commands", "approvals", "notifications"}}
	}},
	{matchName("telegram getUpdates"), telegramGetUpdatesRepairSpec},
	{matchName("telegram webhook"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "A webhook can block polling-based bot updates.", SafeFix: "restart Onibi so startup deletes the webhook", ManualFix: "delete the webhook through Telegram Bot API", FilesTouched: []string{}, Retry: "onibi doctor", Blocks: []string{"Telegram command intake"}}
	}},
	{matchName("bot identity"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Stored bot identity does not match the token, so updates may belong to the wrong bot.", SafeFix: "rotate the bot token and pair again", ManualFix: "verify BotFather token and clear stale bot identity only after confirming ownership", FilesTouched: []string{"secret store", "onibi.sqlite"}, Retry: "onibi rotate-token", Blocks: []string{"Telegram commands", "approvals"}}
	}},
	{matchName("doctor mode"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Doctor mode selection is invalid, so checks may use the wrong strictness.", SafeFix: "run with a valid doctor mode", ManualFix: "use auto, preflight, installed, or ci", FilesTouched: []string{}, Retry: "onibi doctor --mode auto"}
	}},
	{matchName("encrypted seed"), encryptedRepairSpec},
	{matchName("mini app url"), miniAppRepairSpec},
	{matchName("secure button"), secureButtonRepairSpec},
	{matchName("webapp roundtrip"), func(_, _ string) repairSpec {
		return repairSpec{Impact: "Onibi has not observed an encrypted Mini App action from this state DB, so device pairing cannot be confirmed.", SafeFix: "open /secure from Telegram and submit a test encrypted action", ManualFix: "re-pair the Mini App seed on the Telegram device", FilesTouched: []string{"Telegram Mini App secure storage", "onibi.sqlite"}, Retry: "/secure status", Blocks: []string{"encrypted command confidence"}}
	}},
}

func botTokenRepairSpec(_, detail string) repairSpec {
	spec := repairSpec{Impact: "Telegram daemon startup, approvals, notifications, and Telegram-created sessions cannot work until token lookup succeeds. [Inference] Existing local shells/agents outside Onibi and any already-running PTY/tmux process keep running at OS level.", SafeFix: "unlock/login to the OS keychain, then rerun the retry command", ManualFix: "rotate token only if the stored token is invalid or inaccessible after keychain repair", FilesTouched: []string{}, Retry: "onibi doctor, onibi up, or onibi run", Blocks: []string{"Telegram daemon startup", "approvals", "notifications", "Telegram-created sessions"}}
	if strings.Contains(strings.ToLower(detail), "timeout") || strings.Contains(strings.ToLower(detail), "deadline") {
		spec.Code = "bot_token_timeout"
		return spec
	}
	spec.Code = "bot_token_missing"
	spec.SafeFix = "complete setup or rotate the bot token"
	spec.ManualFix = "store a valid BotFather token in keychain; use dotenv only as an explicit fallback"
	spec.FilesTouched = []string{"secret store"}
	spec.Retry = "onibi setup --complete"
	return spec
}

func unixSocketRepairSpec(_, detail string) repairSpec {
	if strings.Contains(detail, "not listening") {
		return repairSpec{Impact: "The daemon socket exists but is stale or unreachable.", SafeFix: "remove stale socket with doctor fix", ManualFix: "stop stale daemon processes and remove the socket file", FilesTouched: []string{"onibi.sock"}, Retry: "onibi doctor --fix", Blocks: []string{"daemon RPC", "Telegram controls"}, Fixable: true}
	}
	return repairSpec{Impact: "No daemon socket is available, so local CLI/Telegram controls cannot reach the daemon.", SafeFix: "start or install the daemon", ManualFix: "run onibi run for foreground use", FilesTouched: []string{}, Retry: "onibi install-service or onibi run", Blocks: []string{"daemon RPC", "Telegram controls"}}
}

func hookRepairSpec(_, detail string) repairSpec {
	spec := repairSpec{Impact: "Agent integration hooks may not fire correctly.", SafeFix: "install hooks interactively", ManualFix: "inspect provider hook config before changing it", FilesTouched: []string{"provider hook configs"}, Retry: "onibi install-hooks --interactive", Blocks: []string{"agent approvals", "turn notifications"}}
	switch {
	case strings.Contains(detail, "hash missing"):
		spec.SafeFix = "adopt current hook hash after inspection"
		spec.Retry = "onibi doctor --fix"
		spec.Fixable = true
	case strings.Contains(detail, "tampered"):
		spec.Impact = "A managed hook changed since install; it may be unsafe or broken."
		spec.SafeFix = "do not auto-fix tampered hooks"
		spec.ManualFix = "review the hook file, then uninstall/reinstall if the change is expected"
		spec.Blocks = []string{"trusted hook execution"}
	case strings.Contains(detail, "outdated"):
		spec.SafeFix = "reinstall the hook with the current Onibi binary"
		spec.Retry = "onibi doctor --fix"
		spec.Fixable = true
	case strings.Contains(detail, "schema"):
		spec.Impact = "A provider may reject the hook config at startup."
		spec.SafeFix = "reinstall hooks with the current schema-clean adapter"
		spec.Blocks = []string{"provider startup"}
	}
	return spec
}

func telegramGetUpdatesRepairSpec(_, detail string) repairSpec {
	if strings.Contains(detail, "another getUpdates poller") {
		return repairSpec{Impact: "Telegram update polling is already owned elsewhere, so this daemon cannot receive commands.", SafeFix: "stop the other poller or rotate the bot token", ManualFix: "find the other process/container using getUpdates", FilesTouched: []string{"secret store if rotating token"}, Retry: "onibi rotate-token", Blocks: []string{"Telegram commands", "approvals"}}
	}
	return repairSpec{Impact: "Telegram getUpdates is unhealthy, so commands may not arrive.", SafeFix: "retry after checking network and token state", ManualFix: "inspect Telegram API response details", FilesTouched: []string{}, Retry: "onibi doctor", Blocks: []string{"Telegram commands"}}
}

func encryptedRepairSpec(_, _ string) repairSpec {
	return repairSpec{Impact: "Encrypted mode cannot decrypt or authorize secure actions without a seed.", SafeFix: "create/pair encrypted mode seed through setup", ManualFix: "turn encrypted mode off only if plaintext Telegram transport is acceptable", FilesTouched: []string{"secret store"}, Retry: "onibi setup --enable-encrypted-mode", Blocks: []string{"encrypted prompts", "encrypted approvals"}}
}

func miniAppRepairSpec(_, _ string) repairSpec {
	return repairSpec{Impact: "Encrypted controls may be unavailable from Telegram.", SafeFix: "set a valid HTTPS Mini App URL or localhost dev URL", ManualFix: "host docs/miniapp/index.html and update telegram.mini_app_url", FilesTouched: []string{"config.yaml"}, Retry: "onibi config set telegram.mini_app_url <url>", Blocks: []string{"encrypted controls"}}
}

func secureButtonRepairSpec(_, detail string) repairSpec {
	if strings.Contains(detail, "seed") {
		return encryptedRepairSpec("", detail)
	}
	return miniAppRepairSpec("", detail)
}

func matchName(want string) func(string, string) bool {
	return func(name, _ string) bool { return name == want }
}

func matchPrefix(prefix string) func(string, string) bool {
	return func(name, _ string) bool { return strings.HasPrefix(name, prefix) }
}

func codeFor(name string) string {
	repl := strings.NewReplacer(" ", "_", ".", "", ":", "_", "-", "_")
	return repl.Replace(strings.ToLower(name))
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
	token, ok, err := r.sec.GetWithTimeout(r.ctx, secrets.KeyBotToken, secretLookupTimeout)
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

func (r *runner) checkEncryptedReadiness() {
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil && strings.TrimSpace(cfg.Telegram.EncryptedMode) == "" {
		r.add("encrypted mode", Warn, err.Error())
		return
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Telegram.EncryptedMode))
	if mode == "" {
		mode = "off"
	}
	if mode == "off" {
		r.add("encrypted mode", Pass, "off")
		return
	}
	required := encryptedRequiredStatus(mode)
	seedPresent := false
	if r.sec == nil {
		r.add("encrypted seed", required, "secrets backend unavailable")
	} else {
		seed, ok, err := r.sec.GetWithTimeout(r.ctx, secrets.KeyEnvelopeSeed, secretLookupTimeout)
		switch {
		case err != nil:
			r.add("encrypted seed", Fail, err.Error())
		case !ok || strings.TrimSpace(seed) == "":
			r.add("encrypted seed", required, "missing")
		default:
			seedPresent = true
			r.add("encrypted seed", Pass, "present")
		}
	}
	mini := strings.TrimSpace(cfg.Telegram.MiniAppURL)
	miniOK := false
	switch {
	case mini == "":
		r.add("mini app url", required, "missing")
	case !miniappurl.Allowed(mini):
		r.add("mini app url", required, "must use https or localhost http")
	default:
		miniOK = true
		r.add("mini app url", Pass, "ok")
	}
	if seedPresent && miniOK {
		r.add("secure button", Pass, "available")
	} else {
		r.add("secure button", required, "unavailable; seed and Mini App URL required")
	}
	r.add("plaintext commands", Pass, "blocked: /prompt /send /editprompt /rename")
	if r.db == nil {
		r.add("webapp roundtrip", Warn, "unknown; sqlite db unavailable")
		return
	}
	if _, ok, err := r.db.KVGetString(r.ctx, secureLastActionKey); err != nil {
		r.add("webapp roundtrip", Warn, err.Error())
	} else if !ok {
		r.add("webapp roundtrip", Warn, "never seen; devices paired cannot be verified")
	} else {
		r.add("webapp roundtrip", Pass, "seen")
	}
}

func encryptedRequiredStatus(mode string) Status {
	if mode == "on" {
		return Fail
	}
	return Warn
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

func (r *runner) checkSessionRuntime() {
	if path, err := exec.LookPath("tmux"); err == nil {
		r.add("tmux", Pass, path)
	} else {
		r.add("tmux", Fail, "required for headless/visible session switching")
	}
	cfg, _, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("terminal launcher", Warn, err.Error())
		return
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Terminal.Default)) {
	case "none":
		r.add("terminal launcher", Warn, "disabled; /show will print tmux attach command")
	case "terminal":
		if _, err := exec.LookPath("osascript"); err == nil {
			r.add("terminal launcher", Pass, "Terminal.app via osascript")
		} else {
			r.add("terminal launcher", Warn, "osascript not found")
		}
	case "iterm", "iterm2":
		if _, err := exec.LookPath("osascript"); err != nil {
			r.add("terminal launcher", Warn, "osascript not found")
		} else if macAppExists("iTerm.app", "iTerm2.app") {
			r.add("terminal launcher", Pass, "iTerm2 via osascript")
		} else {
			r.add("terminal launcher", Warn, "iTerm2 not found")
		}
	case "ghostty":
		if _, err := exec.LookPath("ghostty"); err == nil {
			r.add("terminal launcher", Pass, "Ghostty")
		} else if _, err := os.Stat("/Applications/Ghostty.app"); err == nil {
			r.add("terminal launcher", Pass, "Ghostty.app")
		} else {
			r.add("terminal launcher", Warn, "Ghostty not found")
		}
	default:
		if _, err := exec.LookPath("ghostty"); err == nil {
			r.add("terminal launcher", Pass, "auto: Ghostty")
		} else if _, err := os.Stat("/Applications/Ghostty.app"); err == nil {
			r.add("terminal launcher", Pass, "auto: Ghostty.app")
		} else if _, err := exec.LookPath("osascript"); err == nil && macAppExists("iTerm.app", "iTerm2.app") {
			r.add("terminal launcher", Pass, "auto: iTerm2")
		} else if _, err := exec.LookPath("osascript"); err == nil {
			r.add("terminal launcher", Pass, "auto: Terminal.app")
		} else {
			r.add("terminal launcher", Warn, "auto fallback will print tmux attach command")
		}
	}
}

func macAppExists(names ...string) bool {
	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots, home+"/Applications")
	}
	for _, root := range roots {
		for _, name := range names {
			if _, err := os.Stat(root + "/" + name); err == nil {
				return true
			}
		}
	}
	return false
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

func (r *runner) checkAfterUpgradeHooks() {
	r.checkAfterUpgradeConfig()
	r.checkAfterUpgradeDB()
	if r.db == nil {
		return
	}
	checked := 0
	issues := 0
	commandChecked := 0
	commandIssues := 0
	for _, name := range adapters.Names() {
		info := adapters.Registry()[name].Status(r.ctx, r.db)
		if name == "codex" && info.Installed {
			r.add("after-upgrade hook codex trust", Warn, "Codex trust state is manual; run codex and review Onibi hooks before trusting")
		}
		if name == "claude" && info.Installed {
			r.checkAfterUpgradeClaudeRuntime()
		}
		if pluginSourceAdapter(name) && info.Installed && info.Outdated {
			issues++
			r.add("after-upgrade hook "+name, Fail, fmt.Sprintf("plugin source stale: version %s want %s", versionLabel(info.InstalledVersion), common.IntegrationVersion))
		}
		if info.InstallPath == "" || filepath.Ext(info.InstallPath) != ".json" {
			continue
		}
		body, err := os.ReadFile(info.InstallPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		checked++
		if err != nil {
			issues++
			r.add("after-upgrade hook "+name, Fail, err.Error())
			continue
		}
		var cfg any
		if err := json.Unmarshal(body, &cfg); err != nil {
			issues++
			r.add("after-upgrade hook "+name, Fail, "parse "+info.InstallPath+": "+err.Error())
			continue
		}
		fields := legacyMetadataFields(cfg)
		if len(fields) > 0 {
			issues++
			r.add("after-upgrade hook "+name, Fail, "legacy Onibi metadata fields in "+info.InstallPath+": "+strings.Join(fields, ", "))
		}
		if name == "gemini" {
			low := geminiLowTimeouts(cfg)
			if len(low) > 0 {
				issues++
				r.add("after-upgrade hook gemini", Fail, "Gemini hook timeout must be milliseconds: "+strings.Join(low, ", "))
			}
		}
		for _, cmd := range managedHookCommands(cfg, name) {
			commandChecked++
			if path := notifyPathFromCommand(cmd); path == "" {
				commandIssues++
				r.add("after-upgrade hook "+name, Fail, "onibi-notify path missing in managed command")
			} else if _, err := os.Stat(path); err != nil {
				commandIssues++
				r.add("after-upgrade hook "+name, Fail, "onibi-notify missing: "+path)
			}
			if got := common.CommandVersion(cmd); got == "" {
				commandIssues++
				r.add("after-upgrade hook "+name, Fail, "managed command missing "+common.VersionEnv)
			} else if got != common.IntegrationVersion {
				commandIssues++
				r.add("after-upgrade hook "+name, Fail, fmt.Sprintf("managed command version %s want %s", got, common.IntegrationVersion))
			}
		}
	}
	if issues == 0 {
		r.add("after-upgrade hook schema", Pass, fmt.Sprintf("checked %d provider JSON configs", checked))
	}
	if commandChecked > 0 && commandIssues == 0 {
		r.add("after-upgrade hook commands", Pass, fmt.Sprintf("checked %d managed commands", commandChecked))
	}
}

func (r *runner) checkAfterUpgradeConfig() {
	_, meta, err := config.Load(r.opts.Paths)
	if err != nil {
		r.add("after-upgrade config", Fail, err.Error())
		return
	}
	r.add("after-upgrade config", Pass, meta.Path)
}

func (r *runner) checkAfterUpgradeDB() {
	if r.db == nil {
		return
	}
	var version int
	if err := r.db.SQL().QueryRowContext(r.ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		r.add("after-upgrade db schema", Fail, err.Error())
		return
	}
	if version < 1 {
		r.add("after-upgrade db schema", Fail, fmt.Sprintf("version %d want >=1", version))
		return
	}
	missing := missingUpgradeColumns(r.ctx, r.db)
	if len(missing) > 0 {
		r.add("after-upgrade db schema", Fail, "missing columns: "+strings.Join(missing, ", "))
		return
	}
	r.add("after-upgrade db schema", Pass, fmt.Sprintf("version %d", version))
}

func (r *runner) checkAfterUpgradeClaudeRuntime() {
	path, err := exec.LookPath("claude")
	if err != nil {
		r.add("after-upgrade claude runtime", Fail, "claude binary not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		r.add("after-upgrade claude runtime", Fail, strings.TrimSpace(string(out))+": "+err.Error())
		return
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		r.add("after-upgrade claude runtime", Warn, "claude --version returned empty output")
		return
	}
	r.add("after-upgrade claude runtime", Pass, version)
}

func missingUpgradeColumns(ctx context.Context, db *store.DB) []string {
	want := map[string][]string{
		"hooks":     {"version"},
		"approvals": {"reason", "decided_by"},
		"sessions":  {"cmd", "last_activity"},
	}
	var missing []string
	for table, columns := range want {
		have := tableColumns(ctx, db, table)
		for _, column := range columns {
			if !have[column] {
				missing = append(missing, table+"."+column)
			}
		}
	}
	sort.Strings(missing)
	return missing
}

func tableColumns(ctx context.Context, db *store.DB, table string) map[string]bool {
	rows, err := db.SQL().QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return map[string]bool{}
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return map[string]bool{}
		}
		out[name] = true
	}
	return out
}

func pluginSourceAdapter(name string) bool {
	switch name {
	case "amp", "opencode", "pi":
		return true
	default:
		return false
	}
}

func versionLabel(v *string) string {
	if v == nil || *v == "" {
		return "unknown"
	}
	return *v
}

func legacyMetadataFields(v any) []string {
	fields := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == common.VersionField || k == common.GuardField || k == "onibi-managed" {
					fields[k] = true
				}
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(v)
	out := make([]string, 0, len(fields))
	for k := range fields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func geminiLowTimeouts(v any) []string {
	var out []string
	root, _ := v.(map[string]any)
	hooks, _ := root["hooks"].(map[string]any)
	for event, groups := range hooks {
		for _, group := range asSlice(groups) {
			gm, _ := group.(map[string]any)
			for _, hook := range asSlice(gm["hooks"]) {
				hm, _ := hook.(map[string]any)
				if !managedCommand(hm, "gemini") {
					continue
				}
				timeout, ok := numberValue(hm["timeout"])
				if ok && timeout > 0 && timeout < 1000 {
					out = append(out, fmt.Sprintf("%s=%g", event, timeout))
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func managedHookCommands(v any, agent string) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if managedCommand(x, agent) {
				if cmd := commandValue(x); cmd != "" {
					out = append(out, cmd)
				}
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(v)
	return out
}

func managedCommand(m map[string]any, agent string) bool {
	if m[common.GuardField] == true {
		return true
	}
	cmd := commandValue(m)
	return strings.Contains(cmd, "onibi-notify") && strings.Contains(cmd, "--agent "+agent)
}

func commandValue(m map[string]any) string {
	for _, key := range []string{"command", "bash", "powershell"} {
		if s, _ := m[key].(string); s != "" {
			return s
		}
	}
	return ""
}

func notifyPathFromCommand(cmd string) string {
	for _, field := range shellFields(cmd) {
		if strings.Contains(filepath.Base(field), "onibi-notify") {
			return field
		}
	}
	return ""
}

func shellFields(s string) []string {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, b.String())
		b.Reset()
	}
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return out
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func (r *runner) checkTOTP() {
	if r.sec == nil {
		return
	}
	_, ok, err := r.sec.GetWithTimeout(r.ctx, secrets.KeyTOTPSecret, secretLookupTimeout)
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

const (
	secretLookupTimeout = 30 * time.Second
	secureLastActionKey = "secure:last_webapp_action"
)

func (r *runner) checkTelegram() {
	if r.token == "" {
		return
	}
	res, err := telegramProbeToken(r.ctx, r.token, false)
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
		if r.db != nil {
			_ = r.db.ClearTelegramPollerConflict(r.ctx)
		}
		r.add("telegram getUpdates", Pass, res.GetUpdatesDetail)
	} else if res.GetUpdatesDetail != "" {
		if r.db != nil {
			_ = r.db.SetTelegramPollerConflict(r.ctx, res.GetUpdatesDetail)
		}
		status := Warn
		if strings.Contains(res.GetUpdatesDetail, "another getUpdates poller") {
			status = Fail
		}
		r.add("telegram getUpdates", status, res.GetUpdatesDetail)
	}
}
