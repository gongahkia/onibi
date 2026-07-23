package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/capability"
	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return FormatDuration(time.Duration(d)) }

func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be scalar, got %s", n.ShortTag())
	}
	v, err := ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

type Config struct {
	Daemon       Daemon       `yaml:"daemon" json:"daemon"`
	Shell        Shell        `yaml:"shell" json:"shell"`
	Web          Web          `yaml:"web" json:"web"`
	Transport    Transport    `yaml:"transport" json:"transport"`
	Provider     Provider     `yaml:"provider" json:"provider"`
	Terminal     Terminal     `yaml:"terminal" json:"terminal"`
}

type Daemon struct {
	ApprovalTimeout       Duration `yaml:"approval_timeout" json:"approval_timeout"`
	ApprovalSweepInterval Duration `yaml:"approval_sweep_interval" json:"approval_sweep_interval"`
	TurnIdleThreshold     Duration `yaml:"turn_idle_threshold" json:"turn_idle_threshold"`
	TurnIdleInterval      Duration `yaml:"turn_idle_interval" json:"turn_idle_interval"`
	PTYBufferBytes        int      `yaml:"pty_buffer_bytes" json:"pty_buffer_bytes"`
	MaxSubscribers        int      `yaml:"max_subscribers" json:"max_subscribers"`
}

type Shell struct {
	Default string `yaml:"default" json:"default"`
	Login   bool   `yaml:"login" json:"login"`
}

type Terminal struct {
	Default string `yaml:"default" json:"default"`
}

type Web struct {
	ListenAddr string `yaml:"listen_addr" json:"listen_addr"`
	CertDir    string `yaml:"cert_dir" json:"cert_dir"`
}

type Transport struct {
	Mode  string `yaml:"mode" json:"mode"`
	SAddr string `yaml:"saddr" json:"saddr"`
}

type Provider struct {
	Output ProviderOutput `yaml:"output" json:"output"`
}

type ProviderOutput struct {
	MaxChunks int                    `yaml:"max_chunks" json:"max_chunks"`
	MaxBytes  int                    `yaml:"max_bytes" json:"max_bytes"`
	Redaction string                 `yaml:"redaction" json:"redaction"`
	Telegram  ProviderOutputOverride `yaml:"telegram,omitempty" json:"telegram,omitempty"`
}

type ProviderOutputOverride struct {
	MaxChunks int    `yaml:"max_chunks,omitempty" json:"max_chunks,omitempty"`
	MaxBytes  int    `yaml:"max_bytes,omitempty" json:"max_bytes,omitempty"`
	Redaction string `yaml:"redaction,omitempty" json:"redaction,omitempty"`
}

type LoadMeta struct {
	Path     string
	Exists   bool
	Explicit map[string]bool
}

type KeyInfo struct {
	Key         string
	Default     string
	Current     string
	Explicit    bool
	Description string
}

func Default() Config {
	return Config{
		Daemon: Daemon{
			ApprovalTimeout:       Duration(5 * time.Minute),
			ApprovalSweepInterval: Duration(15 * time.Second),
			TurnIdleThreshold:     Duration(3 * time.Second),
			TurnIdleInterval:      Duration(500 * time.Millisecond),
			PTYBufferBytes:        64 * 1024,
			MaxSubscribers:        32,
		},
		Shell: Shell{
			Default: "auto",
			Login:   true,
		},
		Web:       Web{ListenAddr: ":8443"},
		Transport: Transport{Mode: "lan"},
		Provider:  Provider{Output: ProviderOutput{MaxChunks: 8, MaxBytes: 24 * 1024, Redaction: "default"}},
		Terminal: Terminal{
			Default: "auto",
		},
	}
}

func Load(paths Paths) (Config, LoadMeta, error) {
	path := paths.Config
	if path == "" {
		path = filepath.Join(paths.StateDir, "config.yaml")
	}
	cfg := Default()
	meta := LoadMeta{Path: path, Explicit: map[string]bool{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, meta, nil
	}
	if err != nil {
		return cfg, meta, err
	}
	meta.Exists = true
	return loadBytes(path, b, cfg, meta)
}

func loadBytes(path string, b []byte, cfg Config, meta LoadMeta) (Config, LoadMeta, error) {
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); errors.Is(err, io.EOF) {
		return cfg, meta, nil
	} else if err != nil {
		return cfg, meta, fmt.Errorf("parse %s: %w", path, err)
	}
	applyRaw(&cfg, &meta, raw)
	if err := cfg.Validate(); err != nil {
		return cfg, meta, fmt.Errorf("validate %s: %w", path, err)
	}
	return cfg, meta, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		return errors.New("config path required")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (c Config) Validate() error {
	checks := []struct {
		name string
		got  time.Duration
		min  time.Duration
		max  time.Duration
	}{
		{"daemon.approval_timeout", c.Daemon.ApprovalTimeout.Std(), 10 * time.Second, 5 * time.Minute},
		{"daemon.approval_sweep_interval", c.Daemon.ApprovalSweepInterval.Std(), time.Second, 5 * time.Minute},
		{"daemon.turn_idle_threshold", c.Daemon.TurnIdleThreshold.Std(), time.Second, 30 * time.Minute},
		{"daemon.turn_idle_interval", c.Daemon.TurnIdleInterval.Std(), 100 * time.Millisecond, 10 * time.Second},
	}
	for _, x := range checks {
		if x.got < x.min || x.got > x.max {
			return fmt.Errorf("%s must be between %s and %s", x.name, x.min, x.max)
		}
	}
	if c.Daemon.PTYBufferBytes < 4096 || c.Daemon.PTYBufferBytes > 10*1024*1024 {
		return fmt.Errorf("daemon.pty_buffer_bytes must be between 4096 and 10485760")
	}
	if c.Daemon.MaxSubscribers < 1 || c.Daemon.MaxSubscribers > 4096 {
		return fmt.Errorf("daemon.max_subscribers must be between 1 and 4096")
	}
	if c.Provider.Output.MaxChunks < 1 || c.Provider.Output.MaxChunks > 100 {
		return fmt.Errorf("provider.output.max_chunks must be between 1 and 100")
	}
	if c.Provider.Output.MaxBytes < 512 || c.Provider.Output.MaxBytes > 1024*1024 {
		return fmt.Errorf("provider.output.max_bytes must be between 512 and 1048576")
	}
	switch strings.ToLower(strings.TrimSpace(c.Provider.Output.Redaction)) {
	case "default", "strict", "off":
	default:
		return fmt.Errorf("provider.output.redaction must be default, strict, or off")
	}
	for _, name := range providerOutputProviderNames() {
		ov := providerOutputOverride(c.Provider.Output, name)
		if err := validateProviderOutputOverride(name, ov); err != nil {
			return err
		}
	}
	if err := validateShellDefault(c.Shell.Default); err != nil {
		return err
	}
	if strings.TrimSpace(c.Web.ListenAddr) == "" {
		return fmt.Errorf("web.listen_addr required")
	}
	mode := strings.ToLower(strings.TrimSpace(c.Transport.Mode))
	switch {
	case mode == "tailscale":
		return fmt.Errorf("transport.mode=%q is no longer supported; set transport.mode to tailscale-private or another supported transport", mode)
	case mode == "cloudflare-named":
		return fmt.Errorf("transport.mode=%q has been removed; set transport.mode to cloudflare-quick or a private transport", mode)
	case capability.IsV1WebTransport(mode), capability.IsInternalWebTransport(mode), capability.IsV1ProviderTransport(mode):
	case mode == "email" || mode == "sms" || mode == "apns" || mode == "gotify" || mode == "ntfy" || mode == "pushover" || mode == "signal" || mode == "zulip" || mode == "discord" || mode == "slack" || mode == "matrix":
		return fmt.Errorf("transport.mode=%q is no longer supported; use web push or telegram", mode)
	default:
		return fmt.Errorf("transport.mode must be a supported web transport or telegram")
	}
	switch strings.ToLower(strings.TrimSpace(c.Terminal.Default)) {
	case "auto", "ghostty", "none":
	case "iterm", "iterm2", "terminal":
		return fmt.Errorf("terminal.default=%q is no longer supported; use ghostty or none", c.Terminal.Default)
	default:
		return fmt.Errorf("terminal.default must be one of auto, ghostty, or none")
	}
	return nil
}

func validateShellDefault(v string) error {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return fmt.Errorf("shell.default required")
	}
	if strings.Contains(v, "/") {
		if !filepath.IsAbs(v) {
			return fmt.Errorf("shell.default path must be absolute")
		}
		if !validShellName(filepath.Base(v)) {
			return fmt.Errorf("shell.default path must end in a supported shell name")
		}
		return nil
	}
	if v == "auto" || validShellName(v) {
		return nil
	}
	return fmt.Errorf("shell.default must be auto, zsh, bash, fish, sh, nu, pwsh, powershell, ksh, ksh93, mksh, oksh, tcsh, csh, dash, ash, busybox, or an absolute path")
}

func validShellName(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "zsh", "bash", "fish", "sh",
		"nu", "nushell", "pwsh", "powershell",
		"ksh", "ksh93", "mksh", "oksh",
		"tcsh", "csh",
		"dash", "ash", "busybox", "busybox-sh":
		return true
	default:
		return false
	}
}

func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("duration required")
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("duration %q must be Go duration syntax like 5m or integer seconds", s)
	}
	if n < 0 {
		return 0, errors.New("duration must be non-negative")
	}
	return time.Duration(n) * time.Second, nil
}

func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", d/time.Second)
	}
	if d%time.Millisecond == 0 {
		return fmt.Sprintf("%dms", d/time.Millisecond)
	}
	return d.String()
}

func Set(cfg *Config, key, value string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "daemon.approval_timeout":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.ApprovalTimeout = Duration(d)
	case "daemon.approval_sweep_interval":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.ApprovalSweepInterval = Duration(d)
	case "daemon.turn_idle_threshold":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.TurnIdleThreshold = Duration(d)
	case "daemon.turn_idle_interval":
		d, err := ParseDuration(value)
		if err != nil {
			return err
		}
		cfg.Daemon.TurnIdleInterval = Duration(d)
	case "daemon.pty_buffer_bytes":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("daemon.pty_buffer_bytes must be integer bytes")
		}
		cfg.Daemon.PTYBufferBytes = n
	case "daemon.max_subscribers":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("daemon.max_subscribers must be integer")
		}
		cfg.Daemon.MaxSubscribers = n
	case "shell.default":
		cfg.Shell.Default = strings.TrimSpace(value)
	case "shell.login":
		v, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("shell.login must be boolean")
		}
		cfg.Shell.Login = v
	case "web.listen_addr":
		cfg.Web.ListenAddr = strings.TrimSpace(value)
	case "web.cert_dir":
		cfg.Web.CertDir = strings.TrimSpace(value)
	case "transport.mode":
		cfg.Transport.Mode = strings.ToLower(strings.TrimSpace(value))
	case "transport.saddr":
		cfg.Transport.SAddr = strings.TrimSpace(value)
	case "provider.output.max_chunks":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("provider.output.max_chunks must be integer")
		}
		cfg.Provider.Output.MaxChunks = n
	case "provider.output.max_bytes":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("provider.output.max_bytes must be integer bytes")
		}
		cfg.Provider.Output.MaxBytes = n
	case "provider.output.redaction":
		cfg.Provider.Output.Redaction = strings.ToLower(strings.TrimSpace(value))
	case "terminal.default":
		cfg.Terminal.Default = strings.ToLower(strings.TrimSpace(value))
	default:
		if handled, err := setProviderOutputOverride(&cfg.Provider.Output, strings.ToLower(strings.TrimSpace(key)), value); handled {
			if err != nil {
				return err
			}
			break
		}
		return fmt.Errorf("unknown config key %q", key)
	}
	return cfg.Validate()
}

func Get(cfg Config, key string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "daemon.approval_timeout":
		return cfg.Daemon.ApprovalTimeout.String(), nil
	case "daemon.approval_sweep_interval":
		return cfg.Daemon.ApprovalSweepInterval.String(), nil
	case "daemon.turn_idle_threshold":
		return cfg.Daemon.TurnIdleThreshold.String(), nil
	case "daemon.turn_idle_interval":
		return cfg.Daemon.TurnIdleInterval.String(), nil
	case "daemon.pty_buffer_bytes":
		return strconv.Itoa(cfg.Daemon.PTYBufferBytes), nil
	case "daemon.max_subscribers":
		return strconv.Itoa(cfg.Daemon.MaxSubscribers), nil
	case "shell.default":
		return cfg.Shell.Default, nil
	case "shell.login":
		return strconv.FormatBool(cfg.Shell.Login), nil
	case "web.listen_addr":
		return cfg.Web.ListenAddr, nil
	case "web.cert_dir":
		return cfg.Web.CertDir, nil
	case "transport.mode":
		return cfg.Transport.Mode, nil
	case "transport.saddr":
		return cfg.Transport.SAddr, nil
	case "provider.output.max_chunks":
		return strconv.Itoa(cfg.Provider.Output.MaxChunks), nil
	case "provider.output.max_bytes":
		return strconv.Itoa(cfg.Provider.Output.MaxBytes), nil
	case "provider.output.redaction":
		return cfg.Provider.Output.Redaction, nil
	case "terminal.default":
		return cfg.Terminal.Default, nil
	default:
		if handled, value := getProviderOutputOverride(cfg.Provider.Output, strings.ToLower(strings.TrimSpace(key))); handled {
			return value, nil
		}
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func Keys(cfg Config, meta LoadMeta) []KeyInfo {
	def := Default()
	rows := []KeyInfo{
		{"daemon.approval_timeout", def.Daemon.ApprovalTimeout.String(), cfg.Daemon.ApprovalTimeout.String(), meta.Explicit["daemon.approval_timeout"], "approval request lifetime before default-deny"},
		{"daemon.approval_sweep_interval", def.Daemon.ApprovalSweepInterval.String(), cfg.Daemon.ApprovalSweepInterval.String(), meta.Explicit["daemon.approval_sweep_interval"], "how often pending approvals are expired"},
		{"daemon.turn_idle_threshold", def.Daemon.TurnIdleThreshold.String(), cfg.Daemon.TurnIdleThreshold.String(), meta.Explicit["daemon.turn_idle_threshold"], "fallback silence window before turn-complete"},
		{"daemon.turn_idle_interval", def.Daemon.TurnIdleInterval.String(), cfg.Daemon.TurnIdleInterval.String(), meta.Explicit["daemon.turn_idle_interval"], "fallback idle poll cadence"},
		{"daemon.pty_buffer_bytes", strconv.Itoa(def.Daemon.PTYBufferBytes), strconv.Itoa(cfg.Daemon.PTYBufferBytes), meta.Explicit["daemon.pty_buffer_bytes"], "bytes retained for /peek text rendering"},
		{"daemon.max_subscribers", strconv.Itoa(def.Daemon.MaxSubscribers), strconv.Itoa(cfg.Daemon.MaxSubscribers), meta.Explicit["daemon.max_subscribers"], "maximum approval event subscribers"},
		{"shell.default", def.Shell.Default, cfg.Shell.Default, meta.Explicit["shell.default"], "shell launched by `onibi shell`: auto, shell name, or absolute path"},
		{"shell.login", strconv.FormatBool(def.Shell.Login), strconv.FormatBool(cfg.Shell.Login), meta.Explicit["shell.login"], "start `onibi shell` as login+interactive when supported"},
		{"terminal.default", def.Terminal.Default, cfg.Terminal.Default, meta.Explicit["terminal.default"], "visible session handover: auto/ghostty opens Ghostty on macOS; otherwise manual tmux attach"},
		{"transport.mode", def.Transport.Mode, cfg.Transport.Mode, meta.Explicit["transport.mode"], "transport: lan, tailscale-private, wireguard, zerotier, cloudflare-quick, ngrok, auto, or telegram"},
		{"transport.saddr", def.Transport.SAddr, cfg.Transport.SAddr, meta.Explicit["transport.saddr"], "optional transport service address"},
		{"provider.output.max_chunks", strconv.Itoa(def.Provider.Output.MaxChunks), strconv.Itoa(cfg.Provider.Output.MaxChunks), meta.Explicit["provider.output.max_chunks"], "maximum provider reply chunks per session command"},
		{"provider.output.max_bytes", strconv.Itoa(def.Provider.Output.MaxBytes), strconv.Itoa(cfg.Provider.Output.MaxBytes), meta.Explicit["provider.output.max_bytes"], "maximum provider reply bytes per session command"},
		{"provider.output.redaction", def.Provider.Output.Redaction, cfg.Provider.Output.Redaction, meta.Explicit["provider.output.redaction"], "provider output redaction: default, strict, or off"},
		{"web.cert_dir", def.Web.CertDir, cfg.Web.CertDir, meta.Explicit["web.cert_dir"], "local HTTPS certificate directory"},
		{"web.listen_addr", def.Web.ListenAddr, cfg.Web.ListenAddr, meta.Explicit["web.listen_addr"], "local web cockpit listen address"},
	}
	for _, provider := range providerOutputProviderNames() {
		ov := providerOutputOverride(cfg.Provider.Output, provider)
		prefix := "provider.output." + provider
		rows = append(rows,
			KeyInfo{prefix + ".max_chunks", "inherit", inheritedInt(ov.MaxChunks), meta.Explicit[prefix+".max_chunks"], provider + " reply chunk override; inherit uses provider.output.max_chunks"},
			KeyInfo{prefix + ".max_bytes", "inherit", inheritedInt(ov.MaxBytes), meta.Explicit[prefix+".max_bytes"], provider + " reply byte override; inherit uses provider.output.max_bytes"},
			KeyInfo{prefix + ".redaction", "inherit", inheritedString(ov.Redaction), meta.Explicit[prefix+".redaction"], provider + " redaction override; inherit uses provider.output.redaction"},
		)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	return rows
}

type rawConfig struct {
	Daemon       rawDaemon       `yaml:"daemon"`
	Shell        rawShell        `yaml:"shell"`
	Web          rawWeb          `yaml:"web"`
	Transport    rawTransport    `yaml:"transport"`
	Provider     rawProvider     `yaml:"provider"`
	Terminal     rawTerminal     `yaml:"terminal"`
	Update       rawLegacyUpdate `yaml:"update"`
}

type rawDaemon struct {
	ApprovalTimeout       *Duration `yaml:"approval_timeout"`
	ApprovalSweepInterval *Duration `yaml:"approval_sweep_interval"`
	TurnIdleThreshold     *Duration `yaml:"turn_idle_threshold"`
	TurnIdleInterval      *Duration `yaml:"turn_idle_interval"`
	PTYBufferBytes        *int      `yaml:"pty_buffer_bytes"`
	MaxSubscribers        *int      `yaml:"max_subscribers"`
}

type rawShell struct {
	LegacyMinDuration *Duration `yaml:"min_duration"`
	Default           *string   `yaml:"default"`
	Login             *bool     `yaml:"login"`
}

type rawTerminal struct {
	Default *string `yaml:"default"`
}

type rawLegacyUpdate struct {
	Auto    *bool   `yaml:"auto"`
	Channel *string `yaml:"channel"`
}

type rawWeb struct {
	ListenAddr *string `yaml:"listen_addr"`
	CertDir    *string `yaml:"cert_dir"`
}

type rawTransport struct {
	Mode  *string `yaml:"mode"`
	SAddr *string `yaml:"saddr"`
}

type rawProvider struct {
	Output rawProviderOutput `yaml:"output"`
}

type rawProviderOutput struct {
	MaxChunks *int                      `yaml:"max_chunks"`
	MaxBytes  *int                      `yaml:"max_bytes"`
	Redaction *string                   `yaml:"redaction"`
	Telegram  rawProviderOutputOverride `yaml:"telegram"`
}

type rawProviderOutputOverride struct {
	MaxChunks *int    `yaml:"max_chunks"`
	MaxBytes  *int    `yaml:"max_bytes"`
	Redaction *string `yaml:"redaction"`
}

func applyRaw(cfg *Config, meta *LoadMeta, raw rawConfig) {
	if raw.Daemon.ApprovalTimeout != nil {
		cfg.Daemon.ApprovalTimeout = *raw.Daemon.ApprovalTimeout
		meta.Explicit["daemon.approval_timeout"] = true
	}
	if raw.Daemon.ApprovalSweepInterval != nil {
		cfg.Daemon.ApprovalSweepInterval = *raw.Daemon.ApprovalSweepInterval
		meta.Explicit["daemon.approval_sweep_interval"] = true
	}
	if raw.Daemon.TurnIdleThreshold != nil {
		cfg.Daemon.TurnIdleThreshold = *raw.Daemon.TurnIdleThreshold
		meta.Explicit["daemon.turn_idle_threshold"] = true
	}
	if raw.Daemon.TurnIdleInterval != nil {
		cfg.Daemon.TurnIdleInterval = *raw.Daemon.TurnIdleInterval
		meta.Explicit["daemon.turn_idle_interval"] = true
	}
	if raw.Daemon.PTYBufferBytes != nil {
		cfg.Daemon.PTYBufferBytes = *raw.Daemon.PTYBufferBytes
		meta.Explicit["daemon.pty_buffer_bytes"] = true
	}
	if raw.Daemon.MaxSubscribers != nil {
		cfg.Daemon.MaxSubscribers = *raw.Daemon.MaxSubscribers
		meta.Explicit["daemon.max_subscribers"] = true
	}
	if raw.Shell.Default != nil {
		cfg.Shell.Default = strings.TrimSpace(*raw.Shell.Default)
		meta.Explicit["shell.default"] = true
	}
	if raw.Shell.Login != nil {
		cfg.Shell.Login = *raw.Shell.Login
		meta.Explicit["shell.login"] = true
	}
	if raw.Web.ListenAddr != nil {
		cfg.Web.ListenAddr = strings.TrimSpace(*raw.Web.ListenAddr)
		meta.Explicit["web.listen_addr"] = true
	}
	if raw.Web.CertDir != nil {
		cfg.Web.CertDir = strings.TrimSpace(*raw.Web.CertDir)
		meta.Explicit["web.cert_dir"] = true
	}
	if raw.Transport.Mode != nil {
		cfg.Transport.Mode = strings.ToLower(strings.TrimSpace(*raw.Transport.Mode))
		meta.Explicit["transport.mode"] = true
	}
	if raw.Transport.SAddr != nil {
		cfg.Transport.SAddr = strings.TrimSpace(*raw.Transport.SAddr)
		meta.Explicit["transport.saddr"] = true
	}
	if raw.Provider.Output.MaxChunks != nil {
		cfg.Provider.Output.MaxChunks = *raw.Provider.Output.MaxChunks
		meta.Explicit["provider.output.max_chunks"] = true
	}
	if raw.Provider.Output.MaxBytes != nil {
		cfg.Provider.Output.MaxBytes = *raw.Provider.Output.MaxBytes
		meta.Explicit["provider.output.max_bytes"] = true
	}
	if raw.Provider.Output.Redaction != nil {
		cfg.Provider.Output.Redaction = strings.ToLower(strings.TrimSpace(*raw.Provider.Output.Redaction))
		meta.Explicit["provider.output.redaction"] = true
	}
	applyRawProviderOutputOverride(&cfg.Provider.Output.Telegram, meta, "telegram", raw.Provider.Output.Telegram)
	if raw.Terminal.Default != nil {
		cfg.Terminal.Default = strings.ToLower(strings.TrimSpace(*raw.Terminal.Default))
		meta.Explicit["terminal.default"] = true
	}
}

func providerOutputProviderNames() []string {
	return []string{"telegram"}
}

func providerOutputOverride(out ProviderOutput, provider string) ProviderOutputOverride {
	switch provider {
	case "telegram":
		return out.Telegram
	default:
		return ProviderOutputOverride{}
	}
}

func providerOutputOverridePtr(out *ProviderOutput, provider string) *ProviderOutputOverride {
	switch provider {
	case "telegram":
		return &out.Telegram
	default:
		return nil
	}
}

func validateProviderOutputOverride(provider string, ov ProviderOutputOverride) error {
	prefix := "provider.output." + provider
	if ov.MaxChunks < 0 || ov.MaxChunks > 100 {
		return fmt.Errorf("%s.max_chunks must be inherit or between 1 and 100", prefix)
	}
	if ov.MaxBytes < 0 || ov.MaxBytes > 1024*1024 || (ov.MaxBytes > 0 && ov.MaxBytes < 512) {
		return fmt.Errorf("%s.max_bytes must be inherit or between 512 and 1048576", prefix)
	}
	switch strings.ToLower(strings.TrimSpace(ov.Redaction)) {
	case "", "default", "strict", "off":
		return nil
	default:
		return fmt.Errorf("%s.redaction must be inherit, default, strict, or off", prefix)
	}
}

func setProviderOutputOverride(out *ProviderOutput, key, value string) (bool, error) {
	const prefix = "provider.output."
	if !strings.HasPrefix(key, prefix) {
		return false, nil
	}
	rest := strings.TrimPrefix(key, prefix)
	parts := strings.Split(rest, ".")
	if len(parts) != 2 {
		return false, nil
	}
	ov := providerOutputOverridePtr(out, parts[0])
	if ov == nil {
		return false, nil
	}
	v := strings.ToLower(strings.TrimSpace(value))
	clear := v == "" || v == "inherit"
	switch parts[1] {
	case "max_chunks":
		if clear {
			ov.MaxChunks = 0
			return true, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return true, fmt.Errorf("%s must be integer or inherit", key)
		}
		ov.MaxChunks = n
	case "max_bytes":
		if clear {
			ov.MaxBytes = 0
			return true, nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return true, fmt.Errorf("%s must be integer bytes or inherit", key)
		}
		ov.MaxBytes = n
	case "redaction":
		if clear {
			ov.Redaction = ""
			return true, nil
		}
		ov.Redaction = v
	default:
		return false, nil
	}
	return true, nil
}

func getProviderOutputOverride(out ProviderOutput, key string) (bool, string) {
	const prefix = "provider.output."
	if !strings.HasPrefix(key, prefix) {
		return false, ""
	}
	rest := strings.TrimPrefix(key, prefix)
	parts := strings.Split(rest, ".")
	if len(parts) != 2 {
		return false, ""
	}
	if providerOutputOverridePtr(&out, parts[0]) == nil {
		return false, ""
	}
	ov := providerOutputOverride(out, parts[0])
	switch parts[1] {
	case "max_chunks":
		return true, inheritedInt(ov.MaxChunks)
	case "max_bytes":
		return true, inheritedInt(ov.MaxBytes)
	case "redaction":
		return true, inheritedString(ov.Redaction)
	default:
		return false, ""
	}
}

func inheritedInt(v int) string {
	if v == 0 {
		return "inherit"
	}
	return strconv.Itoa(v)
}

func inheritedString(v string) string {
	if strings.TrimSpace(v) == "" {
		return "inherit"
	}
	return v
}

func applyRawProviderOutputOverride(dst *ProviderOutputOverride, meta *LoadMeta, provider string, raw rawProviderOutputOverride) {
	prefix := "provider.output." + provider
	if raw.MaxChunks != nil {
		dst.MaxChunks = *raw.MaxChunks
		meta.Explicit[prefix+".max_chunks"] = true
	}
	if raw.MaxBytes != nil {
		dst.MaxBytes = *raw.MaxBytes
		meta.Explicit[prefix+".max_bytes"] = true
	}
	if raw.Redaction != nil {
		dst.Redaction = strings.ToLower(strings.TrimSpace(*raw.Redaction))
		meta.Explicit[prefix+".redaction"] = true
	}
}
