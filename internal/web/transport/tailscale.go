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
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	tailscaleHTTPSCap       = "https"
	tailscaleFunnelCap      = "funnel"
	tailscaleFunnelPortsCap = "https://tailscale.com/cap/funnel-ports"
	TailscaleBinEnv         = "ONIBI_TAILSCALE_BIN"
)

type Tailscale struct {
	Bin      string
	private  bool
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
	AllowFunnel map[string]bool             `json:"AllowFunnel"`
	Foreground  map[string]serveStatusLayer `json:"Foreground"`
	Web         map[string]json.RawMessage  `json:"Web"`
}

type serveStatusLayer struct {
	AllowFunnel map[string]bool `json:"AllowFunnel"`
}

func NewTailscale() *Tailscale {
	return &Tailscale{Bin: TailscaleBin(), runner: execCommandRunner{}, lookPath: exec.LookPath}
}

func NewTailscalePrivate() *Tailscale {
	return &Tailscale{Bin: TailscaleBin(), private: true, runner: execCommandRunner{}, lookPath: exec.LookPath}
}

func TailscaleBin() string {
	if bin := strings.TrimSpace(os.Getenv(TailscaleBinEnv)); bin != "" {
		return bin
	}
	return "tailscale"
}

func (t *Tailscale) Detect(ctx context.Context) (bool, error) {
	if err := t.Check(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (t *Tailscale) Check(ctx context.Context) error {
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
	if t.private {
		_, err := tailscalePrivateURL(st)
		return err
	}
	if !st.Self.hasCap(tailscaleFunnelCap) {
		return errors.New("tailscale Funnel node attribute is not enabled")
	}
	if !st.Self.funnelPortAllowed(443) {
		return errors.New("tailscale Funnel is not allowed on public port 443")
	}
	return nil
}

func (t *Tailscale) Enable(ctx context.Context, localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	if err := t.Check(ctx); err != nil {
		return err
	}
	target := fmt.Sprintf("https+insecure://localhost:%d", localPort)
	command := "funnel"
	if t.private {
		command = "serve"
	}
	if _, err := t.run(ctx, command, "--bg", target); err != nil {
		return fmt.Errorf("tailscale %s --bg: %w", command, err)
	}
	return t.waitForURL(ctx)
}

func (t *Tailscale) Disable(ctx context.Context) error {
	command := "funnel"
	if t.private {
		command = "serve"
	}
	if _, err := t.run(ctx, command, "--bg", "off"); err != nil {
		return Diagnostic(DiagCleanup, "tailscale", fmt.Sprintf("tailscale %s --bg off failed", command), err)
	}
	return nil
}

func (t *Tailscale) URL(ctx context.Context) (string, error) {
	if t.private {
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
	out, err := t.run(ctx, "funnel", "status", "--json")
	if err == nil {
		return funnelURLFromServeStatus(out)
	}
	funnelErr := err
	out, err = t.run(ctx, "serve", "status", "--json")
	if err != nil {
		return "", fmt.Errorf("tailscale funnel status --json: %v; tailscale serve status --json: %w", funnelErr, err)
	}
	return funnelURLFromServeStatus(out)
}

func (t *Tailscale) status(ctx context.Context) (tailscaleStatus, error) {
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

func (t *Tailscale) waitForURL(ctx context.Context) error {
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
			command := "Funnel"
			if t.private {
				command = "Serve"
			}
			return Diagnostic(DiagActivationLag, "tailscale", "tailscale "+command+" did not become active", nil)
		case <-tick.C:
		}
	}
}

func (t *Tailscale) run(ctx context.Context, args ...string) ([]byte, error) {
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

func (p *tailscalePeer) funnelPortAllowed(port int) bool {
	if p == nil {
		return false
	}
	for cap := range p.CapMap {
		if funnelCapAllowsPort(cap, port) {
			return true
		}
	}
	for _, cap := range p.Capabilities {
		if funnelCapAllowsPort(cap, port) {
			return true
		}
	}
	return false
}

func funnelCapAllowsPort(cap string, port int) bool {
	if !strings.HasPrefix(cap, tailscaleFunnelPortsCap) {
		return false
	}
	u, err := url.Parse(cap)
	if err != nil {
		return false
	}
	return portListContains(u.Query().Get("ports"), port)
}

func portListContains(list string, want int) bool {
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 == nil && err2 == nil && want >= lo && want <= hi {
				return true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err == nil && n == want {
			return true
		}
	}
	return false
}

func funnelURLFromServeStatus(body []byte) (string, error) {
	var st serveStatus
	if err := json.Unmarshal(body, &st); err != nil {
		return "", Diagnostic(DiagURLParse, "tailscale", "parse tailscale serve status", err)
	}
	hostPorts := activeFunnelHostPorts(st)
	if len(hostPorts) == 0 {
		return "", errors.New("tailscale serve status has no active Funnel")
	}
	sort.SliceStable(hostPorts, func(i, j int) bool {
		_, pi := splitHostPort(hostPorts[i])
		_, pj := splitHostPort(hostPorts[j])
		if pi == "443" && pj != "443" {
			return true
		}
		if pi != "443" && pj == "443" {
			return false
		}
		return hostPorts[i] < hostPorts[j]
	})
	host, port := splitHostPort(hostPorts[0])
	if host == "" {
		return "", fmt.Errorf("invalid Funnel hostport %q", hostPorts[0])
	}
	if port == "" || port == "443" {
		return "https://" + strings.TrimSuffix(host, ".") + "/", nil
	}
	return "https://" + net.JoinHostPort(strings.TrimSuffix(host, "."), port) + "/", nil
}

func activeFunnelHostPorts(st serveStatus) []string {
	var out []string
	for hp, on := range st.AllowFunnel {
		if on {
			out = append(out, hp)
		}
	}
	for _, layer := range st.Foreground {
		for hp, on := range layer.AllowFunnel {
			if on {
				out = append(out, hp)
			}
		}
	}
	return out
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
