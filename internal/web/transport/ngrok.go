package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	NgrokBinEnv          = "ONIBI_NGROK_BIN"
	NgrokAuthtokenEnv    = "ONIBI_NGROK_AUTHTOKEN"
	NgrokDomainEnv       = "ONIBI_NGROK_DOMAIN"
	NgrokAgentAPIEnv     = "ONIBI_NGROK_AGENT_API"
	NgrokSecretAuthtoken = "onibi.ngrok.token.v1"
	ngrokAgentTokenEnv   = "NGROK_AUTHTOKEN"
	ngrokProvider        = "ngrok"
	ngrokDefaultAPI      = "http://127.0.0.1:4040"
	ngrokActivationWait  = 20 * time.Second
)

type Ngrok struct {
	Bin       string
	Authtoken string
	Domain    string
	AgentAPI  string
	Client    *http.Client
	runner    processRunner
	lookPath  func(string) (string, error)
	mu        sync.Mutex
	process   managedProcess
	tunnel    string
	publicURL string
}

func NewNgrokFromEnv() *Ngrok {
	api := strings.TrimRight(strings.TrimSpace(os.Getenv(NgrokAgentAPIEnv)), "/")
	if api == "" {
		api = ngrokDefaultAPI
	}
	return &Ngrok{
		Bin:       ngrokBin(),
		Authtoken: ngrokAuthtoken(),
		Domain:    strings.TrimSpace(os.Getenv(NgrokDomainEnv)),
		AgentAPI:  api,
		Client:    &http.Client{Timeout: 2 * time.Second},
		runner:    execProcessRunner{},
		lookPath:  exec.LookPath,
	}
}

func ngrokBin() string {
	if bin := strings.TrimSpace(os.Getenv(NgrokBinEnv)); bin != "" {
		return bin
	}
	return "ngrok"
}

func (n *Ngrok) Check(context.Context) error {
	if err := checkBinary(n.bin(), n.lookPath, ngrokProvider); err != nil {
		return err
	}
	if strings.TrimSpace(n.Domain) != "" && strings.TrimSpace(n.Authtoken) == "" {
		return Diagnostic(DiagAuthMissing, ngrokProvider, NgrokAuthtokenEnv+" required when "+NgrokDomainEnv+" is set", nil)
	}
	return nil
}

func (n *Ngrok) Enable(ctx context.Context, localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	if err := n.Check(ctx); err != nil {
		return err
	}
	args := []string{"http", fmt.Sprintf("https://localhost:%d", localPort)}
	if strings.TrimSpace(n.Domain) != "" {
		args = append(args, "--domain", strings.TrimSpace(n.Domain))
	}
	runner := n.runner
	if runner == nil {
		runner = execProcessRunner{}
	}
	var proc managedProcess
	var err error
	if token := strings.TrimSpace(n.Authtoken); token != "" {
		envRunner, ok := runner.(envProcessRunner)
		if !ok {
			return errors.New("ngrok runner does not support secret env")
		}
		proc, err = envRunner.StartEnv(ctx, []string{ngrokAgentTokenEnv + "=" + token}, n.bin(), args...)
	} else {
		proc, err = runner.Start(ctx, n.bin(), args...)
	}
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.process = proc
	n.mu.Unlock()
	tunnel, err := n.waitForTunnel(ctx, localPort, ngrokActivationWait)
	if err != nil {
		_ = proc.Kill()
		return err
	}
	n.mu.Lock()
	n.tunnel = tunnel.Name
	n.publicURL = strings.TrimRight(tunnel.PublicURL, "/")
	n.mu.Unlock()
	return nil
}

func (n *Ngrok) URL(context.Context) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.publicURL == "" {
		return "", Diagnostic(DiagActivationLag, ngrokProvider, "ngrok URL not ready", nil)
	}
	return n.publicURL, nil
}

func (n *Ngrok) Disable(ctx context.Context) error {
	n.mu.Lock()
	proc := n.process
	tunnel := n.tunnel
	n.process = nil
	n.tunnel = ""
	n.publicURL = ""
	n.mu.Unlock()
	var shutdownErr error
	if tunnel != "" {
		if err := n.deleteTunnel(ctx, tunnel); err != nil {
			shutdownErr = Diagnostic(DiagCleanup, ngrokProvider, "Agent API tunnel shutdown failed", err)
		}
	}
	if err := stopManagedProcess(ctx, ngrokProvider, proc); err != nil && shutdownErr == nil {
		shutdownErr = err
	}
	return shutdownErr
}

func (n *Ngrok) bin() string {
	if strings.TrimSpace(n.Bin) == "" {
		return "ngrok"
	}
	return strings.TrimSpace(n.Bin)
}

func (n *Ngrok) waitForTunnel(ctx context.Context, localPort int, timeout time.Duration) (ngrokTunnel, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		tunnel, ok, err := n.discoverTunnel(ctx, localPort)
		if err == nil && ok {
			return tunnel, nil
		}
		select {
		case <-ctx.Done():
			return ngrokTunnel{}, ctx.Err()
		case <-timer.C:
			if err != nil {
				return ngrokTunnel{}, Diagnostic(DiagActivationLag, ngrokProvider, "Agent API discovery failed", err)
			}
			return ngrokTunnel{}, Diagnostic(DiagActivationLag, ngrokProvider, "Agent API did not expose an HTTPS tunnel", nil)
		case <-tick.C:
		}
	}
}

func (n *Ngrok) discoverTunnel(ctx context.Context, localPort int) (ngrokTunnel, bool, error) {
	client := n.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(n.agentAPI(), "/")+"/api/tunnels", nil)
	if err != nil {
		return ngrokTunnel{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ngrokTunnel{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ngrokTunnel{}, false, fmt.Errorf("agent API status %d", resp.StatusCode)
	}
	var body ngrokTunnelList
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ngrokTunnel{}, false, err
	}
	return selectNgrokTunnel(body.Tunnels, localPort, n.Domain)
}

func (n *Ngrok) deleteTunnel(ctx context.Context, name string) error {
	client := n.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	u := strings.TrimRight(n.agentAPI(), "/") + "/api/tunnels/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("agent API shutdown status %d", resp.StatusCode)
	}
	return nil
}

func (n *Ngrok) agentAPI() string {
	if strings.TrimSpace(n.AgentAPI) == "" {
		return ngrokDefaultAPI
	}
	return strings.TrimRight(strings.TrimSpace(n.AgentAPI), "/")
}

type ngrokTunnelList struct {
	Tunnels []ngrokTunnel `json:"tunnels"`
}

type ngrokTunnel struct {
	Name      string            `json:"name"`
	PublicURL string            `json:"public_url"`
	Proto     string            `json:"proto"`
	Config    ngrokTunnelConfig `json:"config"`
}

type ngrokTunnelConfig struct {
	Addr string `json:"addr"`
}

func selectNgrokTunnel(tunnels []ngrokTunnel, localPort int, domain string) (ngrokTunnel, bool, error) {
	wantAddr := fmt.Sprintf("https://localhost:%d", localPort)
	wantDomain := strings.TrimSpace(domain)
	insecure := false
	for _, t := range tunnels {
		if t.PublicURL == "" {
			continue
		}
		if wantDomain != "" {
			u, err := url.Parse(t.PublicURL)
			if err != nil {
				return ngrokTunnel{}, false, Diagnostic(DiagURLParse, ngrokProvider, "invalid Agent API public_url", err)
			}
			if strings.EqualFold(u.Hostname(), wantDomain) {
				if u.Scheme != "https" {
					insecure = true
					continue
				}
				return t, true, nil
			}
			continue
		}
		if t.Config.Addr == "" || strings.EqualFold(t.Config.Addr, wantAddr) {
			u, err := url.Parse(t.PublicURL)
			if err != nil {
				return ngrokTunnel{}, false, Diagnostic(DiagURLParse, ngrokProvider, "invalid Agent API public_url", err)
			}
			if u.Scheme != "https" {
				insecure = true
				continue
			}
			return t, true, nil
		}
	}
	if insecure {
		return ngrokTunnel{}, false, Diagnostic(DiagURLParse, ngrokProvider, "Agent API exposed only non-HTTPS public_url", nil)
	}
	return ngrokTunnel{}, false, nil
}
