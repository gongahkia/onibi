package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/web"
	"github.com/gongahkia/onibi/internal/web/transport"
)

const tailnetHealthTimeout = 750 * time.Millisecond

type tailnetStatusJSON struct {
	BackendState string                     `json:"BackendState"`
	Self         *tailnetPeerJSON           `json:"Self"`
	Peer         map[string]tailnetPeerJSON `json:"Peer"`
}

type tailnetPeerJSON struct {
	DNSName  string `json:"DNSName"`
	HostName string `json:"HostName"`
}

func tailnetStatusOrDefault(fn func(context.Context) ([]byte, error)) func(context.Context) ([]byte, error) {
	if fn != nil {
		return fn
	}
	return defaultTailnetStatus
}

func tailnetHealthOrDefault(fn func(context.Context, string) (bool, error)) func(context.Context, string) (bool, error) {
	if fn != nil {
		return fn
	}
	return defaultTailnetHealth
}

func defaultTailnetStatus(ctx context.Context) ([]byte, error) {
	bin := strings.TrimSpace(transport.TailscaleBin())
	if bin == "" {
		bin = "tailscale"
	}
	cmd := exec.CommandContext(ctx, bin, "status", "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("tailscale status --json: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("tailscale status --json: %w", err)
	}
	return out, nil
}

func defaultTailnetHealth(ctx context.Context, baseURL string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, tailnetHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/healthz", nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, err
	}
	return body.OK, nil
}

func (d *Daemon) tailnetPeerSessions(ctx context.Context) ([]web.SessionSummary, error) {
	statusFn := d.tailnetStatus
	if statusFn == nil {
		statusFn = defaultTailnetStatus
	}
	body, err := statusFn(ctx)
	if err != nil {
		return nil, err
	}
	var st tailnetStatusJSON
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}
	if st.BackendState != "Running" {
		return nil, nil
	}
	self := tailnetDNSHost("")
	if st.Self != nil {
		self = tailnetDNSHost(st.Self.DNSName)
	}
	peers := make([]tailnetPeerJSON, 0, len(st.Peer))
	for _, peer := range st.Peer {
		peers = append(peers, peer)
	}
	sort.SliceStable(peers, func(i, j int) bool {
		return tailnetPeerName(peers[i]) < tailnetPeerName(peers[j])
	})
	healthFn := d.tailnetHealth
	if healthFn == nil {
		healthFn = defaultTailnetHealth
	}
	out := make([]web.SessionSummary, 0, len(peers))
	for _, peer := range peers {
		remoteURL, ok := tailnetPeerURL(peer.DNSName)
		if !ok {
			continue
		}
		if tailnetDNSHost(peer.DNSName) == self {
			continue
		}
		healthy, err := healthFn(ctx, remoteURL)
		if err != nil || !healthy {
			if err != nil {
				d.Log.Debug("tailnet peer health failed", "peer", remoteURL, "err", err)
			}
			continue
		}
		name := tailnetPeerName(peer)
		out = append(out, web.SessionSummary{
			ID:           "remote:" + tailnetDNSHost(peer.DNSName),
			Agent:        "onibi",
			CWD:          remoteURL,
			RoleRequired: "remote",
			Remote:       true,
			PeerName:     name,
			RemoteURL:    remoteURL,
		})
	}
	return out, nil
}

func tailnetPeerURL(dnsName string) (string, bool) {
	host := tailnetDNSHost(dnsName)
	if host == "" || strings.ContainsAny(host, `/\@:`) {
		return "", false
	}
	u := url.URL{Scheme: "https", Host: host, Path: "/"}
	return u.String(), true
}

func tailnetDNSHost(dnsName string) string {
	return strings.TrimSuffix(strings.TrimSpace(dnsName), ".")
}

func tailnetPeerName(peer tailnetPeerJSON) string {
	if name := strings.TrimSpace(peer.HostName); name != "" {
		return name
	}
	host := tailnetDNSHost(peer.DNSName)
	if i := strings.Index(host, "."); i > 0 {
		return host[:i]
	}
	return host
}
