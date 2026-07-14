package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/onibi/internal/fleet"
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
	Online   bool   `json:"Online"`
}

func tailnetStatusOrDefault(fn func(context.Context) ([]byte, error)) func(context.Context) ([]byte, error) {
	if fn != nil {
		return fn
	}
	return defaultTailnetStatus
}

func tailnetHealthOrDefault(fn func(context.Context, string, fleet.Host) (bool, error)) func(context.Context, string, fleet.Host) (bool, error) {
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

func defaultTailnetHealth(ctx context.Context, baseURL string, host fleet.Host) (bool, error) {
	if endpoint, ok := meshEndpointURL(host.Endpoint); !ok || endpoint != baseURL {
		return false, fmt.Errorf("mesh health host endpoint mismatch")
	}
	nonce, err := newTailnetHealthNonce()
	if err != nil {
		return false, err
	}
	request := fleet.MeshHealthRequest{Version: fleet.ProtocolVersion, Nonce: nonce, SentAt: time.Now().UTC()}
	if err := request.Validate(); err != nil {
		return false, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, tailnetHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/fleet/mesh-health", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var response fleet.MeshHealthResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return false, err
	}
	return validateTailnetHealthResponse(request, response, host)
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
	eligible, err := d.eligibleMeshHosts(ctx)
	if err != nil || len(eligible) == 0 {
		return nil, err
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
		if !peer.Online {
			continue
		}
		remoteURL, ok := tailnetPeerURL(peer.DNSName)
		if !ok {
			continue
		}
		if tailnetDNSHost(peer.DNSName) == self {
			continue
		}
		host, ok := eligible[remoteURL]
		if !ok {
			continue
		}
		healthy, err := healthFn(ctx, remoteURL, host)
		if err != nil || !healthy {
			if err != nil {
				d.Log.Debug("tailnet peer health failed", "peer", remoteURL, "err", err)
			}
			continue
		}
		name := tailnetPeerName(peer)
		out = append(out, web.SessionSummary{
			ID:           "remote:" + host.ID,
			HostID:       host.ID,
			Agent:        "onibi",
			CWD:          remoteURL,
			LastActivity: formatWebSessionTime(time.Now()),
			RoleRequired: "remote",
			Remote:       true,
			PeerName:     name,
			RemoteURL:    remoteURL,
		})
	}
	return out, nil
}

func (d *Daemon) eligibleMeshHosts(ctx context.Context) (map[string]fleet.Host, error) {
	if d.DB == nil {
		return nil, nil
	}
	ownerID, err := d.DB.FleetOwnerID(ctx)
	if err != nil {
		return nil, err
	}
	hosts, err := d.DB.FleetHostList(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]fleet.Host)
	for _, host := range hosts {
		host = host.Normalized()
		if host.OwnerID != ownerID || host.State != fleet.HostStateActive {
			continue
		}
		endpoint, ok := meshEndpointURL(host.Endpoint)
		if !ok {
			continue
		}
		out[endpoint] = host
	}
	return out, nil
}

func meshEndpointURL(endpoint fleet.Endpoint) (string, bool) {
	if endpoint.Kind != fleet.EndpointMesh || endpoint.Validate() != nil {
		return "", false
	}
	u, err := url.Parse(endpoint.URL)
	if err != nil || (u.Path != "" && u.Path != "/") {
		return "", false
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = "/"
	u.RawPath = ""
	return u.String(), true
}

func newTailnetHealthNonce() (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(nonce), nil
}

func validateTailnetHealthResponse(request fleet.MeshHealthRequest, response fleet.MeshHealthResponse, host fleet.Host) (bool, error) {
	host = host.Normalized()
	if err := host.Validate(); err != nil {
		return false, err
	}
	if err := response.Validate(); err != nil {
		return false, err
	}
	if response.HostID != host.ID || response.Nonce != request.Nonce {
		return false, nil
	}
	now := time.Now().UTC()
	if skew := now.Sub(response.SentAt.UTC()); skew > fleet.MeshHealthSkew || skew < -fleet.MeshHealthSkew {
		return false, nil
	}
	public, err := base64.RawURLEncoding.DecodeString(host.IdentityPublic)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return false, fmt.Errorf("invalid mesh host identity")
	}
	signature, err := base64.RawURLEncoding.DecodeString(response.Signature)
	if err != nil {
		return false, nil
	}
	return ed25519.Verify(ed25519.PublicKey(public), fleet.MeshHealthSigningPayload(request, response), signature), nil
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
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(dnsName), "."))
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
