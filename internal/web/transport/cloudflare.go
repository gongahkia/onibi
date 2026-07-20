package transport

import (
	"context"
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
	CloudflaredBinEnv        = "ONIBI_CLOUDFLARED_BIN"
	cloudflareQuickProvider  = "cloudflare-quick"
	cloudflareActivationWait = 20 * time.Second
	cloudflareReadySubstring = "registered tunnel connection"
)

var tryCloudflareURLRe = regexp.MustCompile(`https://[^\s]+`)

type CloudflareQuick struct {
	Bin          string
	runner       processRunner
	lookPath     func(string) (string, error)
	mu           sync.Mutex
	process      managedProcess
	processExit  <-chan error
	exitObserved bool
	exitErr      error
	publicURL    string
}

func NewCloudflareQuick() *CloudflareQuick {
	return &CloudflareQuick{Bin: cloudflaredBin(), runner: execProcessRunner{}, lookPath: exec.LookPath}
}

func cloudflaredBin() string {
	if bin := strings.TrimSpace(os.Getenv(CloudflaredBinEnv)); bin != "" {
		return bin
	}
	return "cloudflared"
}

func (c *CloudflareQuick) Check(context.Context) error {
	if err := checkBinary(c.Bin, c.lookPath, cloudflareQuickProvider); err != nil {
		return err
	}
	c.mu.Lock()
	proc := c.process
	publicURL := c.publicURL
	exitObserved := c.exitObserved
	exitErr := c.exitErr
	exitCh := c.processExit
	c.mu.Unlock()
	if proc == nil {
		return nil
	}
	if publicURL == "" {
		return Diagnostic(DiagActivationLag, cloudflareQuickProvider, "quick tunnel state is incomplete", nil)
	}
	if exitObserved {
		return Diagnostic(DiagActivationLag, cloudflareQuickProvider, "quick tunnel process exited", exitErr)
	}
	if exitCh == nil {
		return Diagnostic(DiagActivationLag, cloudflareQuickProvider, "quick tunnel process state is unavailable", nil)
	}
	select {
	case err := <-exitCh:
		c.mu.Lock()
		if c.process == proc {
			c.exitObserved = true
			c.exitErr = err
		}
		c.mu.Unlock()
		return Diagnostic(DiagActivationLag, cloudflareQuickProvider, "quick tunnel process exited", err)
	default:
		return nil
	}
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
	proc, err := runner.Start(ctx, c.bin(), "tunnel", "--url", fmt.Sprintf("https://localhost:%d", localPort), "--no-tls-verify")
	if err != nil {
		return err
	}
	exitCh := watchProcessExit(proc)
	c.mu.Lock()
	c.process = proc
	c.processExit = exitCh
	c.exitObserved = false
	c.exitErr = nil
	c.mu.Unlock()
	publicURL, err := waitForQuickTunnelReady(ctx, proc.Lines(), exitCh, cloudflareActivationWait)
	if err != nil {
		_ = c.Disable(context.Background())
		return err
	}
	go drainProcessLines(proc)
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
	c.processExit = nil
	c.exitObserved = false
	c.exitErr = nil
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

func parseTryCloudflareURL(line string) (string, bool) {
	for _, raw := range tryCloudflareURLRe.FindAllString(line, -1) {
		raw = strings.Trim(raw, "()[]{}<>,;.")
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			continue
		}
		if port := u.Port(); port != "" && port != "443" {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(u.Hostname()), ".trycloudflare.com") {
			continue
		}
		return strings.TrimRight(u.String(), "/"), true
	}
	return "", false
}

func waitForQuickTunnelReady(ctx context.Context, lines <-chan string, exitCh <-chan error, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	publicURL := ""
	registered := false
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
			return "", Diagnostic(DiagActivationLag, cloudflareQuickProvider, "quick tunnel did not become ready", nil)
		case err := <-exitCh:
			return "", processExitError(cloudflareQuickProvider, err)
		case line, ok := <-lines:
			if !ok {
				continue
			}
			if url, ok := parseTryCloudflareURL(line); ok {
				publicURL = url
			}
			if strings.Contains(strings.ToLower(line), cloudflareReadySubstring) {
				registered = true
			}
			if publicURL != "" && registered {
				return publicURL, nil
			}
		}
	}
}

func watchProcessExit(proc managedProcess) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- proc.Wait()
		close(done)
	}()
	return done
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

func drainProcessLines(proc managedProcess) {
	if proc == nil {
		return
	}
	for range proc.Lines() {
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
