package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	tailscaleHTTPSCap = "https"
	TailscaleBinEnv   = "ONIBI_TAILSCALE_BIN"
)

type TailscalePrivate struct {
	Bin      string
	runner   commandRunner
	lookPath func(string) (string, error)
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

type tailscaleStatus struct {
	BackendState string         `json:"BackendState"`
	Self         *tailscalePeer `json:"Self"`
}

type tailscalePeer struct {
	DNSName      string                     `json:"DNSName"`
	CapMap       map[string]json.RawMessage `json:"CapMap"`
	Capabilities []string                   `json:"Capabilities"`
}

type serveStatus struct {
	Web map[string]json.RawMessage `json:"Web"`
}

func NewTailscalePrivate() *TailscalePrivate {
	return &TailscalePrivate{Bin: TailscaleBin(), runner: execCommandRunner{}, lookPath: exec.LookPath}
}

func TailscaleBin() string {
	if bin := strings.TrimSpace(os.Getenv(TailscaleBinEnv)); bin != "" {
		return bin
	}
	return "tailscale"
}

func (t *TailscalePrivate) Detect(ctx context.Context) (bool, error) {
	if err := t.Check(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (t *TailscalePrivate) Check(ctx context.Context) error {
	if err := checkBinary(t.Bin, t.lookPath, "tailscale"); err != nil {
		return err
	}
	st, err := t.status(ctx)
	if err != nil {
		return err
	}
	if st.BackendState != "Running" {
		return fmt.Errorf("tailscale backend state is %q, want Running", st.BackendState)
	}
	if st.Self == nil {
		return errors.New("tailscale status missing Self")
	}
	if !st.Self.hasCap(tailscaleHTTPSCap) {
		return errors.New("tailscale HTTPS is not enabled")
	}
	_, err = tailscalePrivateURL(st)
	return err
}

func (t *TailscalePrivate) Enable(ctx context.Context, localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	if err := t.Check(ctx); err != nil {
		return err
	}
	target := fmt.Sprintf("https+insecure://localhost:%d", localPort)
	if _, err := t.run(ctx, "serve", "--bg", target); err != nil {
		return fmt.Errorf("tailscale serve --bg: %w", err)
	}
	return t.waitForURL(ctx)
}

func (t *TailscalePrivate) Disable(ctx context.Context) error {
	if _, err := t.run(ctx, "serve", "--bg", "off"); err != nil {
		return Diagnostic(DiagCleanup, "tailscale", "tailscale serve --bg off failed", err)
	}
	return nil
}

func (t *TailscalePrivate) URL(ctx context.Context) (string, error) {
	st, err := t.status(ctx)
	if err != nil {
		return "", err
	}
	out, err := t.run(ctx, "serve", "status", "--json")
	if err != nil {
		return "", fmt.Errorf("tailscale serve status --json: %w", err)
	}
	return privateURLFromServeStatus(st, out)
}

func (t *TailscalePrivate) status(ctx context.Context) (tailscaleStatus, error) {
	out, err := t.run(ctx, "status", "--json")
	if err != nil {
		return tailscaleStatus{}, fmt.Errorf("tailscale status --json: %w", err)
	}
	var st tailscaleStatus
	if err := json.Unmarshal(out, &st); err != nil {
		return tailscaleStatus{}, fmt.Errorf("parse tailscale status: %w", err)
	}
	return st, nil
}

func (t *TailscalePrivate) waitForURL(ctx context.Context) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := t.URL(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return Diagnostic(DiagActivationLag, "tailscale", "tailscale Serve did not become active", nil)
		case <-tick.C:
		}
	}
}

func (t *TailscalePrivate) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := strings.TrimSpace(t.Bin)
	if bin == "" {
		bin = "tailscale"
	}
	r := t.runner
	if r == nil {
		r = execCommandRunner{}
	}
	out, err := r.Run(ctx, bin, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

func (p *tailscalePeer) hasCap(cap string) bool {
	if p == nil {
		return false
	}
	if _, ok := p.CapMap[cap]; ok {
		return true
	}
	for _, got := range p.Capabilities {
		if got == cap {
			return true
		}
	}
	return false
}

func privateURLFromServeStatus(status tailscaleStatus, body []byte) (string, error) {
	base, err := tailscalePrivateURL(status)
	if err != nil {
		return "", err
	}
	var serve serveStatus
	if err := json.Unmarshal(body, &serve); err != nil {
		return "", Diagnostic(DiagURLParse, "tailscale", "parse tailscale serve status", err)
	}
	want, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	for hostPort := range serve.Web {
		host, port := splitHostPort(hostPort)
		if strings.EqualFold(strings.TrimSuffix(host, "."), want.Hostname()) && (port == "" || port == "443") {
			return base, nil
		}
	}
	return "", errors.New("tailscale serve status has no active private HTTPS handler")
}

func tailscalePrivateURL(status tailscaleStatus) (string, error) {
	if status.Self == nil {
		return "", errors.New("tailscale status missing Self")
	}
	host := strings.TrimSuffix(strings.TrimSpace(status.Self.DNSName), ".")
	if host == "" || strings.ContainsAny(host, " /\\@?#:") {
		return "", errors.New("tailscale status has invalid Self DNSName")
	}
	u, err := url.Parse("https://" + host)
	if err != nil || u.Hostname() != host || u.Port() != "" {
		return "", errors.New("tailscale status has invalid Self DNSName")
	}
	return "https://" + host + "/", nil
}

func splitHostPort(v string) (string, string) {
	host, port, err := net.SplitHostPort(v)
	if err == nil {
		return host, port
	}
	i := strings.LastIndex(v, ":")
	if i <= 0 || i == len(v)-1 {
		return strings.TrimSuffix(v, "."), ""
	}
	return strings.TrimSuffix(v[:i], "."), v[i+1:]
}
