package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	CloudflaredBinEnv             = "ONIBI_CLOUDFLARED_BIN"
	CloudflareTunnelEnv           = "ONIBI_CLOUDFLARE_TUNNEL"
	CloudflareTunnelNameEnv       = "ONIBI_CLOUDFLARE_TUNNEL_NAME"
	CloudflareHostnameEnv         = "ONIBI_CLOUDFLARE_HOSTNAME"
	CloudflareNamedTeardownEnv    = "ONIBI_CLOUDFLARE_TEARDOWN"
	cloudflareQuickProvider       = "cloudflare-quick"
	cloudflareNamedProvider       = "cloudflare-named"
	cloudflareActivationWait      = 20 * time.Second
	cloudflareNamedReadySubstring = "registered tunnel connection"
)

var tryCloudflareURLRe = regexp.MustCompile(`https://[A-Za-z0-9-]+\.trycloudflare\.com\b`)

type CloudflareQuick struct {
	Bin       string
	runner    processRunner
	lookPath  func(string) (string, error)
	mu        sync.Mutex
	process   managedProcess
	publicURL string
}

type CloudflareNamed struct {
	Bin       string
	Tunnel    string
	Hostname  string
	Teardown  bool
	runner    commandRunner
	processes processRunner
	lookPath  func(string) (string, error)
	mu        sync.Mutex
	process   managedProcess
	publicURL string
}

func NewCloudflareQuick() *CloudflareQuick {
	return &CloudflareQuick{Bin: cloudflaredBin(), runner: execProcessRunner{}, lookPath: exec.LookPath}
}

func NewCloudflareNamedFromEnv() *CloudflareNamed {
	tunnel := strings.TrimSpace(os.Getenv(CloudflareTunnelEnv))
	if tunnel == "" {
		tunnel = strings.TrimSpace(os.Getenv(CloudflareTunnelNameEnv))
	}
	return &CloudflareNamed{
		Bin:       cloudflaredBin(),
		Tunnel:    tunnel,
		Hostname:  strings.TrimSpace(os.Getenv(CloudflareHostnameEnv)),
		Teardown:  truthy(os.Getenv(CloudflareNamedTeardownEnv)),
		runner:    execCommandRunner{},
		processes: execProcessRunner{},
		lookPath:  exec.LookPath,
	}
}

func cloudflaredBin() string {
	if bin := strings.TrimSpace(os.Getenv(CloudflaredBinEnv)); bin != "" {
		return bin
	}
	return "cloudflared"
}

func (c *CloudflareQuick) Check(context.Context) error {
	return checkBinary(c.Bin, c.lookPath, cloudflareQuickProvider)
}

func (c *CloudflareQuick) Enable(ctx context.Context, localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	if err := c.Check(ctx); err != nil {
		return err
	}
	runner := c.runner
	if runner == nil {
		runner = execProcessRunner{}
	}
	proc, err := runner.Start(ctx, c.bin(), "tunnel", "--url", fmt.Sprintf("https://localhost:%d", localPort))
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.process = proc
	c.mu.Unlock()
	publicURL, err := waitForProcessURL(ctx, cloudflareQuickProvider, proc, parseTryCloudflareURL, cloudflareActivationWait)
	if err != nil {
		_ = proc.Kill()
		return err
	}
	c.mu.Lock()
	c.publicURL = publicURL
	c.mu.Unlock()
	return nil
}

func (c *CloudflareQuick) URL(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.publicURL == "" {
		return "", Diagnostic(DiagActivationLag, cloudflareQuickProvider, "quick tunnel URL not ready", nil)
	}
	return c.publicURL, nil
}

func (c *CloudflareQuick) Disable(ctx context.Context) error {
	c.mu.Lock()
	proc := c.process
	c.process = nil
	c.publicURL = ""
	c.mu.Unlock()
	return stopManagedProcess(ctx, cloudflareQuickProvider, proc)
}

func (c *CloudflareQuick) bin() string {
	if strings.TrimSpace(c.Bin) == "" {
		return "cloudflared"
	}
	return strings.TrimSpace(c.Bin)
}

func (c *CloudflareNamed) Check(ctx context.Context) error {
	if err := checkBinary(c.bin(), c.lookPath, cloudflareNamedProvider); err != nil {
		return err
	}
	if strings.TrimSpace(c.Tunnel) == "" {
		return Diagnostic(DiagAuthMissing, cloudflareNamedProvider, CloudflareTunnelNameEnv+" required", nil)
	}
	if _, err := namedURL(c.Hostname); err != nil {
		return err
	}
	runner := c.runner
	if runner == nil {
		runner = execCommandRunner{}
	}
	out, err := runner.Run(ctx, c.bin(), "tunnel", "info", c.Tunnel)
	if err != nil {
		if cloudflareAuthError(err) {
			return Diagnostic(DiagAuthMissing, cloudflareNamedProvider, "cloudflared tunnel credentials missing", err)
		}
		return err
	}
	if err := refuseCloudflareRouteCollision(out, c.Tunnel, c.Hostname); err != nil {
		return err
	}
	return nil
}

func (c *CloudflareNamed) Enable(ctx context.Context, localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	if err := c.Check(ctx); err != nil {
		return err
	}
	publicURL, err := namedURL(c.Hostname)
	if err != nil {
		return err
	}
	runner := c.processes
	if runner == nil {
		runner = execProcessRunner{}
	}
	proc, err := runner.Start(ctx, c.bin(), "tunnel", "run", c.Tunnel)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.process = proc
	c.publicURL = publicURL
	c.mu.Unlock()
	if err := waitForNamedTunnelReady(ctx, proc, cloudflareActivationWait); err != nil {
		_ = proc.Kill()
		return err
	}
	return nil
}

func (c *CloudflareNamed) URL(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.publicURL == "" {
		return namedURL(c.Hostname)
	}
	return c.publicURL, nil
}

func (c *CloudflareNamed) Disable(ctx context.Context) error {
	c.mu.Lock()
	proc := c.process
	c.process = nil
	c.publicURL = ""
	c.mu.Unlock()
	return stopManagedProcess(ctx, cloudflareNamedProvider, proc)
}

func (c *CloudflareNamed) bin() string {
	if strings.TrimSpace(c.Bin) == "" {
		return "cloudflared"
	}
	return strings.TrimSpace(c.Bin)
}

func parseTryCloudflareURL(line string) (string, bool) {
	raw := tryCloudflareURLRe.FindString(line)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".trycloudflare.com") {
		return "", false
	}
	return strings.TrimRight(raw, "/"), true
}

func namedURL(hostname string) (string, error) {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return "", Diagnostic(DiagAuthMissing, cloudflareNamedProvider, CloudflareHostnameEnv+" required", nil)
	}
	if strings.Contains(hostname, "://") {
		u, err := url.Parse(hostname)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return "", Diagnostic(DiagURLParse, cloudflareNamedProvider, "hostname must be an HTTPS URL or bare host", err)
		}
		return strings.TrimRight(u.String(), "/"), nil
	}
	if strings.Contains(hostname, "/") {
		return "", Diagnostic(DiagURLParse, cloudflareNamedProvider, "hostname must not include a path", nil)
	}
	return "https://" + hostname, nil
}

func waitForProcessURL(ctx context.Context, provider string, proc managedProcess, parse func(string) (string, bool), timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", Diagnostic(DiagActivationLag, provider, "public URL did not become ready", nil)
		case err := <-done:
			return "", processExitError(provider, err)
		case line, ok := <-proc.Lines():
			if !ok {
				continue
			}
			if url, ok := parse(line); ok {
				return url, nil
			}
		}
	}
}

func waitForNamedTunnelReady(ctx context.Context, proc managedProcess, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return Diagnostic(DiagActivationLag, cloudflareNamedProvider, "named tunnel did not report a registered connection", nil)
		case err := <-done:
			return processExitError(cloudflareNamedProvider, err)
		case line, ok := <-proc.Lines():
			if !ok {
				continue
			}
			if strings.Contains(strings.ToLower(line), cloudflareNamedReadySubstring) {
				return nil
			}
		}
	}
}

func stopManagedProcess(ctx context.Context, provider string, proc managedProcess) error {
	if proc == nil {
		return nil
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return Diagnostic(DiagCleanup, provider, "process kill failed", err)
	}
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case <-ctx.Done():
		return Diagnostic(DiagCleanup, provider, "process wait timed out", ctx.Err())
	case <-done:
		return nil
	}
}

func checkBinary(bin string, lookPath func(string) (string, error), provider string) error {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return Diagnostic(DiagBinaryMissing, provider, "binary name empty", nil)
	}
	if lookPath == nil {
		return nil
	}
	if _, err := lookPath(bin); err != nil {
		return Diagnostic(DiagBinaryMissing, provider, "binary not found in PATH: "+bin, err)
	}
	return nil
}

func cloudflareAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "origin cert") ||
		strings.Contains(msg, "credentials") ||
		strings.Contains(msg, "not logged in") ||
		strings.Contains(msg, "unauthorized")
}

type cloudflareTunnelInfo struct {
	ID       string                  `json:"id"`
	Name     string                  `json:"name"`
	Hostname string                  `json:"hostname"`
	Routes   []cloudflareTunnelRoute `json:"routes"`
}

type cloudflareTunnelRoute struct {
	Hostname string `json:"hostname"`
	TunnelID string `json:"tunnel_id"`
	Tunnel   string `json:"tunnel"`
}

func refuseCloudflareRouteCollision(out []byte, tunnel, hostname string) error {
	host := strings.ToLower(strings.TrimSpace(hostname))
	if host == "" {
		return nil
	}
	var info cloudflareTunnelInfo
	if json.Unmarshal(out, &info) == nil {
		for _, route := range info.Routes {
			if strings.EqualFold(route.Hostname, host) && route.TunnelID != "" && !strings.EqualFold(route.TunnelID, info.ID) {
				return Diagnostic(DiagAuthMissing, cloudflareNamedProvider, "hostname is already routed to another Cloudflare Tunnel; change "+CloudflareHostnameEnv+" or update the DNS route", nil)
			}
		}
		return nil
	}
	text := strings.ToLower(string(out))
	if strings.Contains(text, "already exists") || strings.Contains(text, "already routed") || strings.Contains(text, "route collision") {
		return Diagnostic(DiagAuthMissing, cloudflareNamedProvider, "hostname route collision; choose another hostname or update Cloudflare DNS route", nil)
	}
	return nil
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
