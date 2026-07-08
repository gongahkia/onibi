package transport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/setup"
)

type Mode string

const (
	ModeLAN             Mode = "lan"
	ModeLANLoopback     Mode = "lan-loopback"
	ModeTailscale       Mode = "tailscale"
	ModeWireGuard       Mode = "wireguard"
	ModeTelegram        Mode = "telegram"
	ModeCloudflareQuick Mode = "cloudflare-quick"
	ModeCloudflareNamed Mode = "cloudflare-named"
	ModeNgrok           Mode = "ngrok"
	ModeAuto            Mode = "auto"
)

type DiagnosticCode string

const (
	DiagBinaryMissing  DiagnosticCode = "binary_missing"
	DiagAuthMissing    DiagnosticCode = "auth_missing"
	DiagURLParse       DiagnosticCode = "url_parse_failure"
	DiagActivationLag  DiagnosticCode = "dns_activation_lag"
	DiagCleanup        DiagnosticCode = "cleanup_failure"
	DiagLANUnreachable DiagnosticCode = "lan_unreachable"
)

type DiagnosticError struct {
	Code     DiagnosticCode
	Provider string
	Message  string
	Err      error
}

func (e *DiagnosticError) Error() string {
	if e == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = string(e.Code)
	}
	if e.Provider != "" {
		msg = e.Provider + ": " + msg
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *DiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Diagnostic(code DiagnosticCode, provider, msg string, err error) error {
	return &DiagnosticError{Code: code, Provider: provider, Message: msg, Err: err}
}

type Provider interface {
	Check(context.Context) error
	Enable(context.Context, int) error
	URL(context.Context) (string, error)
	Disable(context.Context) error
}

type ResolverOptions struct {
	Mode         string
	Port         int
	LANHosts     []string
	FallbackHost string
	Logger       *slog.Logger
	Providers    ProviderFactory
}

type ProviderFactory struct {
	Tailscale       func() Provider
	WireGuard       func() Provider
	CloudflareQuick func() Provider
	CloudflareNamed func() Provider
	Ngrok           func() Provider
}

type Resolved struct {
	Mode         Mode
	BaseURL      string
	Port         int
	LANHosts     []string
	FallbackHost string
	cleanup      func(context.Context) error
}

func Resolve(ctx context.Context, opts ResolverOptions) (Resolved, error) {
	mode := NormalizeMode(opts.Mode)
	if mode == "" {
		mode = ModeLAN
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	switch mode {
	case ModeLAN:
		return resolveLAN(opts.Port, opts.LANHosts, opts.FallbackHost)
	case ModeLANLoopback:
		return Resolved{Mode: ModeLANLoopback, Port: opts.Port, LANHosts: []string{"127.0.0.1"}}, nil
	case ModeTailscale:
		return startProvider(ctx, mode, opts.Port, providerOrDefault(opts.Providers.Tailscale, func() Provider { return NewTailscale() }))
	case ModeWireGuard:
		return startProvider(ctx, mode, opts.Port, providerOrDefault(opts.Providers.WireGuard, func() Provider { return NewWireGuardFromEnv() }))
	case ModeCloudflareQuick:
		return startProvider(ctx, mode, opts.Port, providerOrDefault(opts.Providers.CloudflareQuick, func() Provider { return NewCloudflareQuick() }))
	case ModeCloudflareNamed:
		return startProvider(ctx, mode, opts.Port, providerOrDefault(opts.Providers.CloudflareNamed, func() Provider { return NewCloudflareNamedFromEnv() }))
	case ModeNgrok:
		return startProvider(ctx, mode, opts.Port, providerOrDefault(opts.Providers.Ngrok, func() Provider { return NewNgrokFromEnv() }))
	case ModeAuto:
		pt, err := startProvider(ctx, ModeTailscale, opts.Port, providerOrDefault(opts.Providers.Tailscale, func() Provider { return NewTailscale() }))
		if err == nil {
			return pt, nil
		}
		opts.Logger.Warn("tailscale transport unavailable; falling back to lan", "err", err)
		return resolveLAN(opts.Port, opts.LANHosts, opts.FallbackHost)
	default:
		return Resolved{}, fmt.Errorf("unsupported transport %q", opts.Mode)
	}
}

func NormalizeMode(mode string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(mode))) {
	case ModeLAN:
		return ModeLAN
	case ModeLANLoopback:
		return ModeLANLoopback
	case ModeTailscale:
		return ModeTailscale
	case ModeWireGuard:
		return ModeWireGuard
	case ModeTelegram:
		return ModeTelegram
	case ModeCloudflareQuick:
		return ModeCloudflareQuick
	case ModeCloudflareNamed:
		return ModeCloudflareNamed
	case ModeNgrok:
		return ModeNgrok
	case ModeAuto:
		return ModeAuto
	default:
		return ""
	}
}

func SupportedModeList() string {
	return "lan, lan-loopback, tailscale, wireguard, cloudflare-quick, cloudflare-named, ngrok, telegram, matrix, slack, discord, pushover, ntfy, gotify, auto"
}

func IsRelayMode(mode string) bool {
	switch NormalizeMode(mode) {
	case ModeCloudflareQuick, ModeCloudflareNamed, ModeNgrok:
		return true
	default:
		return false
	}
}

func LANResolved(port int, lanHosts []string, fallbackHost string) Resolved {
	return Resolved{Mode: ModeLAN, Port: port, LANHosts: lanHosts, FallbackHost: fallbackHost}
}

func resolveLAN(port int, lanHosts []string, fallbackHost string) (Resolved, error) {
	if err := validateLANReachability(lanHosts, fallbackHost); err != nil {
		return Resolved{}, err
	}
	return LANResolved(port, lanHosts, fallbackHost), nil
}

func validateLANReachability(lanHosts []string, fallbackHost string) error {
	for _, host := range lanHosts {
		if isRoutableLANHost(host) {
			return nil
		}
	}
	if isRoutableLANHost(fallbackHost) {
		return nil
	}
	return Diagnostic(DiagLANUnreachable, "lan", "no routable LAN address detected; managed Wi-Fi, VPN policy, or client isolation may block phone pairing. Connect the Mac to the iPhone hotspot, or use --transport=tailscale, --transport=cloudflare-quick, or --transport=ngrok", nil)
}

func isRoutableLANHost(host string) bool {
	host = strings.Trim(strings.Trim(host, "[]"), ".")
	if host == "" || strings.EqualFold(host, "localhost") {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast()
}

func (r Resolved) URLs(token string) []string {
	if strings.TrimSpace(r.BaseURL) != "" {
		return []string{strings.TrimRight(r.BaseURL, "/") + "/pair/" + token}
	}
	return WebPairURLs(token, r.Port, r.LANHosts, r.FallbackHost)
}

func (r Resolved) TargetURLs() []string {
	if strings.TrimSpace(r.BaseURL) != "" {
		return []string{strings.TrimRight(r.BaseURL, "/")}
	}
	return WebURLs(r.Port, r.LANHosts, r.FallbackHost)
}

func (r Resolved) RedactedBaseURL() string {
	if strings.TrimSpace(r.BaseURL) == "" {
		return ""
	}
	return strings.TrimRight(r.BaseURL, "/")
}

func (r Resolved) Disable(ctx context.Context) error {
	if r.cleanup == nil {
		return nil
	}
	return r.cleanup(ctx)
}

func (r Resolved) Cleanup(ctx context.Context, logger *slog.Logger) {
	if r.cleanup == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	if err := r.cleanup(ctx); err != nil {
		logger.Warn("pair transport cleanup failed", "transport", r.Mode, "diagnostic", diagnosticCode(err), "err", err)
		return
	}
	logger.Info("pair transport cleanup complete", "transport", r.Mode)
}

func WebPairURLs(token string, port int, lanHosts []string, fallback string) []string {
	seen := map[string]bool{}
	add := func(host string, urls []string) []string {
		if host == "" || seen[host] {
			return urls
		}
		seen[host] = true
		return append(urls, setup.WebPairURL("https", host, port, token))
	}
	var urls []string
	for _, host := range lanHosts {
		urls = add(host, urls)
	}
	urls = add(fallback, urls)
	if len(urls) == 0 {
		urls = add("localhost", urls)
	}
	return urls
}

func WebURLs(port int, lanHosts []string, fallback string) []string {
	seen := map[string]bool{}
	add := func(host string, urls []string) []string {
		if host == "" || seen[host] {
			return urls
		}
		seen[host] = true
		return append(urls, "https://"+net.JoinHostPort(host, strconv.Itoa(port)))
	}
	var urls []string
	for _, host := range lanHosts {
		urls = add(host, urls)
	}
	urls = add(fallback, urls)
	if len(urls) == 0 {
		urls = add("localhost", urls)
	}
	return urls
}

func startProvider(ctx context.Context, mode Mode, port int, provider Provider) (Resolved, error) {
	if provider == nil {
		return Resolved{}, fmt.Errorf("%s provider unavailable", mode)
	}
	if err := provider.Enable(ctx, port); err != nil {
		return Resolved{}, err
	}
	baseURL, err := provider.URL(ctx)
	if err != nil {
		_ = provider.Disable(context.Background())
		return Resolved{}, err
	}
	return Resolved{
		Mode:    mode,
		BaseURL: baseURL,
		cleanup: func(ctx context.Context) error {
			return provider.Disable(ctx)
		},
	}, nil
}

func providerOrDefault(fn func() Provider, def func() Provider) Provider {
	if fn != nil {
		return fn()
	}
	return def()
}

func diagnosticCode(err error) string {
	var diag *DiagnosticError
	if errors.As(err, &diag) {
		return string(diag.Code)
	}
	return ""
}

func CleanupWithTimeout(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	if fn == nil {
		return nil
	}
	if timeout <= 0 {
		return fn(ctx)
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return fn(cctx)
}
