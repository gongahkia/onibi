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
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/gongahkia/onibi/internal/adapters"
	"github.com/gongahkia/onibi/internal/apns"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/discord"
	"github.com/gongahkia/onibi/internal/gotify"
	"github.com/gongahkia/onibi/internal/matrix"
	"github.com/gongahkia/onibi/internal/ntfy"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/slack"
	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/web/transport"
	"github.com/gongahkia/onibi/internal/zulip"
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

var (
	newSlackClient = func(appToken, botToken string) *slack.Client {
		return slack.New(appToken, botToken)
	}
	newDiscordClient = func(token string) *discord.Client {
		return discord.New(token)
	}
)

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
		r.add("transport provider", Pass, "Tailscale coverage: unit + fake runner + live opt-in")
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
	case "matrix":
		r.checkMatrixProvider()
	case "slack":
		r.checkSlackProvider()
	case "discord":
		r.checkDiscordProvider()
	case "zulip":
		r.checkZulipProvider()
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
		r.checkNtfyProvider(topic)
	case "gotify":
		r.checkGotifyProvider()
	case "apns":
		r.checkAPNsProvider()
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
	detail := "ngrok coverage: unit + fake agent API + live opt-in"
	if strings.TrimSpace(ng.Authtoken) != "" {
		detail = "ngrok authtoken present; " + detail
	}
	r.add("transport provider", Pass, detail)
}

func (r *runner) checkMatrixProvider() {
	if missing := missingEnv("ONIBI_MATRIX_HOMESERVER", "ONIBI_MATRIX_ACCESS_TOKEN", "ONIBI_MATRIX_ROOM_ID"); len(missing) > 0 {
		r.add("transport provider", Warn, "Matrix missing "+strings.Join(missing, ", "))
		return
	}
	if r.opts.Offline {
		r.add("transport provider", Pass, "Matrix env present; live API checks skipped offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	roomID := strings.TrimSpace(os.Getenv("ONIBI_MATRIX_ROOM_ID"))
	c := matrix.New(os.Getenv("ONIBI_MATRIX_HOMESERVER"), os.Getenv("ONIBI_MATRIX_ACCESS_TOKEN"))
	who, err := c.CheckRoomOwner(ctx, roomID, 50)
	if err != nil {
		r.add("transport provider", Warn, "Matrix room ownership failed: "+err.Error())
		return
	}
	rooms, err := c.JoinedRooms(ctx)
	if err != nil {
		r.add("transport provider", Warn, "Matrix joined-room check failed: "+err.Error())
		return
	}
	if !containsString(rooms.JoinedRooms, roomID) {
		r.add("transport provider", Warn, "Matrix account "+who.UserID+" is not joined to "+roomID)
		return
	}
	encrypted, err := c.IsEncryptedRoom(ctx, roomID)
	if err != nil {
		r.add("transport provider", Warn, "Matrix encryption-state check failed: "+err.Error())
		return
	}
	if encrypted && !envBool("ONIBI_MATRIX_ALLOW_ENCRYPTED") {
		r.add("transport provider", Warn, "Matrix room is encrypted; Onibi refuses encrypted rooms without real Megolm E2EE")
		return
	}
	r.add("transport provider", Pass, "Matrix live API ok: joined room, owner power, encrypted="+fmt.Sprint(encrypted))
}

func (r *runner) checkSlackProvider() {
	if missing := missingEnv("ONIBI_SLACK_APP_TOKEN", "ONIBI_SLACK_BOT_TOKEN"); len(missing) > 0 {
		r.add("transport provider", Warn, "Slack missing "+strings.Join(missing, ", "))
		return
	}
	if r.opts.Offline {
		r.add("transport provider", Pass, "Slack env present; live API checks skipped offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	c := newSlackClient(os.Getenv("ONIBI_SLACK_APP_TOKEN"), os.Getenv("ONIBI_SLACK_BOT_TOKEN"))
	auth, err := c.AuthTest(ctx)
	if err != nil {
		r.add("transport provider", Warn, "Slack auth.test failed: "+err.Error())
		return
	}
	if _, err := c.OpenSocket(ctx); err != nil {
		r.add("transport provider", Warn, "Slack Socket Mode failed: "+err.Error())
		return
	}
	channel := strings.TrimSpace(os.Getenv("ONIBI_SLACK_APPROVAL_CHANNEL"))
	if channel == "" {
		channel = firstCSVEnv("ONIBI_SLACK_ALLOWED_CHANNELS")
	}
	if channel == "" {
		r.add("transport provider", Warn, "Slack auth/socket ok for "+auth.TeamID+"; set ONIBI_SLACK_APPROVAL_CHANNEL or ONIBI_SLACK_ALLOWED_CHANNELS for channel access check")
		return
	}
	info, err := c.ConversationInfo(ctx, channel)
	if err != nil {
		r.add("transport provider", Warn, "Slack channel access failed: "+err.Error())
		return
	}
	if !info.Channel.IsIM && !info.Channel.IsMember {
		r.add("transport provider", Warn, "Slack bot is not a member of "+channel)
		return
	}
	r.add("transport provider", Pass, "Slack live API ok: auth, socket, channel "+channel)
}

func (r *runner) checkDiscordProvider() {
	if missing := missingEnv("ONIBI_DISCORD_TOKEN"); len(missing) > 0 {
		r.add("transport provider", Warn, "Discord missing "+strings.Join(missing, ", "))
		return
	}
	if r.opts.Offline {
		r.add("transport provider", Pass, "Discord env present; live API checks skipped offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	c := newDiscordClient(os.Getenv("ONIBI_DISCORD_TOKEN"))
	app, err := c.CurrentApplication(ctx)
	if err != nil {
		r.add("transport provider", Warn, "Discord application check failed: "+err.Error())
		return
	}
	channel := strings.TrimSpace(os.Getenv("ONIBI_DISCORD_CHANNEL_ID"))
	if channel == "" {
		channel = firstCSVEnv("ONIBI_DISCORD_ALLOWED_IDS")
	}
	if channel == "" {
		r.add("transport provider", Warn, "Discord app "+app.ID+" ok; set ONIBI_DISCORD_CHANNEL_ID for channel permission check")
		return
	}
	ch, err := c.Channel(ctx, channel)
	if err != nil {
		r.add("transport provider", Warn, "Discord channel access failed: "+err.Error())
		return
	}
	slashDetail := ""
	if strings.TrimSpace(os.Getenv("ONIBI_DISCORD_APPLICATION_ID")) != "" || strings.TrimSpace(os.Getenv("ONIBI_DISCORD_GUILD_ID")) != "" {
		appID := strings.TrimSpace(os.Getenv("ONIBI_DISCORD_APPLICATION_ID"))
		if appID == "" {
			appID = app.ID
		}
		commands, err := c.ApplicationCommands(ctx, appID, strings.TrimSpace(os.Getenv("ONIBI_DISCORD_GUILD_ID")))
		if err != nil {
			r.add("transport provider", Warn, "Discord slash command check failed: "+err.Error())
			return
		}
		if !discord.HasOnibiCommand(commands) {
			r.add("transport provider", Warn, "Discord slash command /onibi missing; run onibi discord register")
			return
		}
		slashDetail = ", slash command ok"
	}
	if doctorLiveProbe() {
		if err := c.CreateMessage(ctx, channel, "onibi doctor discord probe"); err != nil {
			r.add("transport provider", Warn, "Discord send permission failed: "+err.Error())
			return
		}
		r.add("transport provider", Pass, "Discord live API ok: application, channel "+ch.ID+", send probe"+slashDetail)
		return
	}
	r.add("transport provider", Pass, "Discord live API ok: application, channel "+ch.ID+slashDetail+"; set ONIBI_DOCTOR_LIVE=1 for send permission probe")
}

func (r *runner) checkZulipProvider() {
	if missing := missingEnv("ONIBI_ZULIP_URL", "ONIBI_ZULIP_EMAIL", "ONIBI_ZULIP_API_KEY", "ONIBI_ZULIP_STREAM"); len(missing) > 0 {
		r.add("transport provider", Warn, "Zulip missing "+strings.Join(missing, ", "))
		return
	}
	if r.opts.Offline {
		r.add("transport provider", Pass, "Zulip env present; live API checks skipped offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	c := newZulipClient(os.Getenv("ONIBI_ZULIP_URL"), os.Getenv("ONIBI_ZULIP_EMAIL"), os.Getenv("ONIBI_ZULIP_API_KEY"))
	q, err := c.RegisterQueue(ctx, zulip.QueueOptions{EventTypes: []string{"message"}, Narrow: [][]string{{"channel", os.Getenv("ONIBI_ZULIP_STREAM")}}})
	if err != nil {
		r.add("transport provider", Warn, "Zulip event queue failed: "+err.Error())
		return
	}
	_ = c.DeleteQueue(ctx, q.QueueID)
	r.add("transport provider", Pass, "Zulip live API ok: event queue on stream "+os.Getenv("ONIBI_ZULIP_STREAM"))
}

func (r *runner) checkNtfyProvider(topic string) {
	if r.opts.Offline {
		r.add("transport provider", Pass, "ntfy topic valid; live publish/subscribe skipped offline")
		return
	}
	if !doctorLiveProbe() {
		r.add("transport provider", Pass, "ntfy topic valid; set ONIBI_DOCTOR_LIVE=1 for publish/subscribe probe")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	c := ntfy.New(os.Getenv("ONIBI_NTFY_BASE_URL"), topic, os.Getenv("ONIBI_NTFY_TOKEN"))
	conn, err := c.SubscribeWSSince(ctx, "all")
	if err != nil {
		r.add("transport provider", Warn, "ntfy subscribe failed: "+err.Error())
		return
	}
	defer conn.CloseNow()
	body := fmt.Sprintf("onibi doctor ntfy probe %d", time.Now().UnixNano())
	if err := c.Publish(ctx, ntfy.Message{Title: "Onibi doctor", Body: body}); err != nil {
		r.add("transport provider", Warn, "ntfy publish failed: "+err.Error())
		return
	}
	if err := waitWebsocketContains(ctx, conn, body); err != nil {
		r.add("transport provider", Warn, "ntfy subscribe did not receive probe: "+err.Error())
		return
	}
	r.add("transport provider", Pass, "ntfy live API ok: publish + WebSocket subscribe")
}

func (r *runner) checkGotifyProvider() {
	if missing := missingEnv("ONIBI_GOTIFY_URL", "ONIBI_GOTIFY_APP_TOKEN"); len(missing) > 0 {
		r.add("transport provider", Warn, "Gotify missing "+strings.Join(missing, ", "))
		return
	}
	if r.opts.Offline {
		r.add("transport provider", Pass, "Gotify env present; live validation skipped offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	c := gotify.New(os.Getenv("ONIBI_GOTIFY_URL"), os.Getenv("ONIBI_GOTIFY_APP_TOKEN"), os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN"))
	if err := c.Validate(ctx); err != nil {
		r.add("transport provider", Warn, "Gotify token validation failed: "+err.Error())
		return
	}
	if !doctorLiveProbe() {
		r.add("transport provider", Pass, "Gotify token validation ok; set ONIBI_DOCTOR_LIVE=1 for send/WS probe")
		return
	}
	body := fmt.Sprintf("onibi doctor gotify probe %d", time.Now().UnixNano())
	var conn *websocket.Conn
	var err error
	if strings.TrimSpace(os.Getenv("ONIBI_GOTIFY_CLIENT_TOKEN")) != "" {
		conn, err = c.SubscribeWS(ctx)
		if err != nil {
			r.add("transport provider", Warn, "Gotify WebSocket subscribe failed: "+err.Error())
			return
		}
		defer conn.CloseNow()
	}
	if err := c.Send(ctx, gotify.Message{Title: "Onibi doctor", Message: body, Priority: 1}); err != nil {
		r.add("transport provider", Warn, "Gotify send probe failed: "+err.Error())
		return
	}
	if conn != nil {
		if err := waitWebsocketContains(ctx, conn, body); err != nil {
			r.add("transport provider", Warn, "Gotify WebSocket did not receive probe: "+err.Error())
			return
		}
		r.add("transport provider", Pass, "Gotify live API ok: token, send, WebSocket")
		return
	}
	r.add("transport provider", Pass, "Gotify live API ok: token + send; set ONIBI_GOTIFY_CLIENT_TOKEN for WS")
}

func (r *runner) checkAPNsProvider() {
	if missing := missingEnv("ONIBI_APNS_KEY_PATH", "ONIBI_APNS_KEY_ID", "ONIBI_APNS_TEAM_ID", "ONIBI_APNS_TOPIC", "ONIBI_APNS_DEVICE_TOKEN"); len(missing) > 0 {
		r.add("transport provider", Warn, "APNs missing "+strings.Join(missing, ", "))
		return
	}
	cfg := apns.Config{KeyPath: os.Getenv("ONIBI_APNS_KEY_PATH"), KeyID: os.Getenv("ONIBI_APNS_KEY_ID"), TeamID: os.Getenv("ONIBI_APNS_TEAM_ID"), Topic: os.Getenv("ONIBI_APNS_TOPIC"), Environment: os.Getenv("ONIBI_APNS_ENV")}
	if err := cfg.Validate(); err != nil {
		r.add("transport provider", Warn, err.Error())
		return
	}
	if r.opts.Offline || !doctorLiveProbe() {
		r.add("transport provider", Pass, "APNs env present; set ONIBI_DOCTOR_LIVE=1 for send probe")
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 8*time.Second)
	defer cancel()
	c, err := apns.New(cfg)
	if err != nil {
		r.add("transport provider", Warn, "APNs client failed: "+err.Error())
		return
	}
	result, err := c.PushApproval(ctx, apns.PushRequest{DeviceToken: os.Getenv("ONIBI_APNS_DEVICE_TOKEN"), Title: "Onibi doctor", Body: "onibi doctor apns probe", ApprovalID: "doctor", TTL: 30 * time.Second})
	if err != nil {
		r.add("transport provider", Warn, fmt.Sprintf("APNs send probe failed: status=%d reason=%s err=%s", result.StatusCode, result.Reason, err.Error()))
		return
	}
	r.add("transport provider", Pass, "APNs live API ok: send")
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

func waitWebsocketContains(ctx context.Context, conn *websocket.Conn, want string) error {
	for {
		_, p, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if strings.Contains(string(p), want) {
			return nil
		}
	}
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
