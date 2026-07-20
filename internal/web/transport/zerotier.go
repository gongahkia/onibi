package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	ZeroTierBinEnv     = "ONIBI_ZEROTIER_BIN"
	ZeroTierNetworkEnv = "ONIBI_ZEROTIER_NETWORK"
	zerotierProvider   = "zerotier"
)

type ZeroTier struct {
	Bin     string
	Network string

	runner         commandRunner
	lookPath       func(string) (string, error)
	interfaceAddrs func(string) ([]net.Addr, error)
	mu             sync.Mutex
	port           int
	host           string
	network        string
	iface          string
}

type zeroTierNetwork struct {
	ID                string
	Name              string
	Status            string
	Type              string
	PortDeviceName    string
	AssignedAddresses []string
}

func NewZeroTierFromEnv() *ZeroTier {
	return &ZeroTier{
		Bin:            zeroTierBin(),
		Network:        strings.TrimSpace(os.Getenv(ZeroTierNetworkEnv)),
		runner:         execCommandRunner{},
		lookPath:       exec.LookPath,
		interfaceAddrs: defaultInterfaceAddrs,
	}
}

func zeroTierBin() string {
	if bin := strings.TrimSpace(os.Getenv(ZeroTierBinEnv)); bin != "" {
		return bin
	}
	return "zerotier-cli"
}

func (z *ZeroTier) Check(ctx context.Context) error {
	host, network, iface, err := z.resolveHost(ctx)
	if err != nil {
		return err
	}
	z.mu.Lock()
	activePort := z.port
	activeHost := z.host
	activeNetwork := z.network
	activeIface := z.iface
	z.mu.Unlock()
	if activePort > 0 && (host != activeHost || network != activeNetwork || iface != activeIface) {
		return Diagnostic(DiagActivationLag, zerotierProvider, "active ZeroTier endpoint changed", nil)
	}
	return nil
}

func (z *ZeroTier) Enable(ctx context.Context, localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	host, network, iface, err := z.resolveHost(ctx)
	if err != nil {
		return err
	}
	z.mu.Lock()
	z.port = localPort
	z.host = host
	z.network = network
	z.iface = iface
	z.mu.Unlock()
	return nil
}

func (z *ZeroTier) URL(ctx context.Context) (string, error) {
	z.mu.Lock()
	host := strings.TrimSpace(z.host)
	port := z.port
	z.mu.Unlock()
	if host == "" {
		var network, iface string
		var err error
		host, network, iface, err = z.resolveHost(ctx)
		if err != nil {
			return "", err
		}
		z.mu.Lock()
		z.host = host
		z.network = network
		z.iface = iface
		port = z.port
		z.mu.Unlock()
	}
	if port <= 0 || port > 65535 {
		return "", errors.New("zerotier local port not set")
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func (z *ZeroTier) Disable(context.Context) error {
	z.mu.Lock()
	z.port = 0
	z.host = ""
	z.network = ""
	z.iface = ""
	z.mu.Unlock()
	return nil
}

func (z *ZeroTier) BindHost(ctx context.Context) (string, error) {
	host, network, iface, err := z.resolveHost(ctx)
	if err != nil {
		return "", err
	}
	z.mu.Lock()
	z.host = host
	z.network = network
	z.iface = iface
	z.mu.Unlock()
	return host, nil
}

func (z *ZeroTier) NetworkID() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return strings.TrimSpace(z.network)
}

func (z *ZeroTier) InterfaceName() string {
	z.mu.Lock()
	defer z.mu.Unlock()
	return strings.TrimSpace(z.iface)
}

func (z *ZeroTier) resolveHost(ctx context.Context) (string, string, string, error) {
	if err := checkBinary(z.bin(), z.lookPath, zerotierProvider); err != nil {
		return "", "", "", err
	}
	if err := z.checkOnline(ctx); err != nil {
		return "", "", "", err
	}
	networks, err := z.zeroTierNetworks(ctx)
	if err != nil {
		return "", "", "", err
	}
	if len(networks) == 0 {
		return "", "", "", errors.New("zerotier-cli listnetworks returned no networks")
	}
	want := strings.TrimSpace(z.Network)
	type candidate struct {
		host    string
		network string
		iface   string
	}
	var candidates []candidate
	var firstHealthErr error
	for _, network := range networks {
		if want != "" && !network.matches(want) {
			continue
		}
		if !network.ready() {
			if want != "" {
				return "", "", "", fmt.Errorf("zerotier network %q status is %q, want OK", network.display(), network.Status)
			}
			continue
		}
		host, iface, err := z.hostForNetwork(network)
		if err == nil {
			candidates = append(candidates, candidate{host: host, network: network.display(), iface: iface})
			continue
		}
		if want != "" {
			return "", "", "", fmt.Errorf("zerotier network %q is not healthy: %w", network.display(), err)
		}
		if firstHealthErr == nil {
			firstHealthErr = fmt.Errorf("zerotier network %q is not healthy: %w", network.display(), err)
		}
	}
	if len(candidates) == 1 {
		return candidates[0].host, candidates[0].network, candidates[0].iface, nil
	}
	if len(candidates) > 1 {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].network != candidates[j].network {
				return candidates[i].network < candidates[j].network
			}
			return candidates[i].host < candidates[j].host
		})
		networks := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			networks = append(networks, candidate.network)
		}
		return "", "", "", fmt.Errorf("multiple ZeroTier networks have routable addresses (%s); set %s to a network ID", strings.Join(networks, ", "), ZeroTierNetworkEnv)
	}
	if want != "" {
		return "", "", "", fmt.Errorf("zerotier network %q is not joined with a routable IP address", want)
	}
	if firstHealthErr != nil {
		return "", "", "", firstHealthErr
	}
	return "", "", "", errors.New("no ZeroTier network with status OK and a routable IP address")
}

func (z *ZeroTier) checkOnline(ctx context.Context) error {
	out, err := z.run(ctx, "info")
	if err != nil {
		return fmt.Errorf("zerotier-cli info: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 5 || fields[0] != "200" || fields[1] != "info" {
		return fmt.Errorf("zerotier-cli info returned unexpected output: %q", strings.TrimSpace(string(out)))
	}
	status := fields[len(fields)-1]
	if !strings.EqualFold(status, "ONLINE") && !strings.EqualFold(status, "TUNNELED") {
		return fmt.Errorf("zerotier-one status is %q, want ONLINE or TUNNELED", status)
	}
	return nil
}

func (z *ZeroTier) zeroTierNetworks(ctx context.Context) ([]zeroTierNetwork, error) {
	out, jsonErr := z.run(ctx, "-j", "listnetworks")
	if jsonErr == nil {
		networks, err := parseZeroTierNetworksJSON(out)
		if err == nil {
			return networks, nil
		}
		if textNetworks, textErr := parseZeroTierNetworksText(out); textErr == nil {
			return textNetworks, nil
		}
		return nil, fmt.Errorf("parse zerotier-cli -j listnetworks: %w", err)
	}
	out, err := z.run(ctx, "listnetworks")
	if err != nil {
		return nil, fmt.Errorf("zerotier-cli -j listnetworks: %v; zerotier-cli listnetworks: %w", jsonErr, err)
	}
	networks, err := parseZeroTierNetworksText(out)
	if err != nil {
		return nil, fmt.Errorf("parse zerotier-cli listnetworks: %w", err)
	}
	return networks, nil
}

func (z *ZeroTier) hostForNetwork(network zeroTierNetwork) (string, string, error) {
	iface := strings.TrimSpace(network.PortDeviceName)
	if host, ok := hostFromAssignedAddresses(network.AssignedAddresses); ok {
		if iface == "" {
			return host, "", nil
		}
		if err := z.verifyInterfaceAddress(iface, host); err != nil {
			return "", "", err
		}
		return host, iface, nil
	}
	if iface == "" {
		return "", "", errors.New("no routable assigned address or interface")
	}
	host, ok := z.hostForInterface(iface)
	if !ok {
		return "", "", fmt.Errorf("interface %s has no routable address", iface)
	}
	return host, iface, nil
}

func (z *ZeroTier) verifyInterfaceAddress(name, host string) error {
	want := net.ParseIP(host)
	if want == nil {
		return fmt.Errorf("invalid assigned address %q", host)
	}
	addrs := z.interfaceAddrs
	if addrs == nil {
		addrs = defaultInterfaceAddrs
	}
	got, err := addrs(name)
	if err != nil {
		return err
	}
	for _, addr := range got {
		if ip := addrIP(addr); ip != nil && ip.Equal(want) {
			return nil
		}
	}
	return fmt.Errorf("interface %s does not contain assigned address %s", name, host)
}

func (z *ZeroTier) hostForInterface(name string) (string, bool) {
	addrs := z.interfaceAddrs
	if addrs == nil {
		addrs = defaultInterfaceAddrs
	}
	got, err := addrs(name)
	if err != nil {
		return "", false
	}
	var ips []net.IP
	for _, addr := range got {
		ip := addrIP(addr)
		if ip == nil || !isRoutableLANHost(ip.String()) {
			continue
		}
		ips = append(ips, ip)
	}
	return preferredPrivateIP(ips)
}

func (z *ZeroTier) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := z.bin()
	runner := z.runner
	if runner == nil {
		runner = execCommandRunner{}
	}
	return runner.Run(ctx, bin, args...)
}

func (z *ZeroTier) bin() string {
	if strings.TrimSpace(z.Bin) == "" {
		return "zerotier-cli"
	}
	return strings.TrimSpace(z.Bin)
}

func parseZeroTierNetworksJSON(out []byte) ([]zeroTierNetwork, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, errors.New("empty JSON output")
	}
	var networks []zeroTierNetwork
	if out[0] == '[' {
		if err := json.Unmarshal(out, &networks); err != nil {
			return nil, err
		}
		return networks, nil
	}
	var wrapped struct {
		Networks []zeroTierNetwork `json:"networks"`
		Result   []zeroTierNetwork `json:"result"`
	}
	if err := json.Unmarshal(out, &wrapped); err == nil {
		switch {
		case wrapped.Networks != nil:
			return wrapped.Networks, nil
		case wrapped.Result != nil:
			return wrapped.Result, nil
		}
	}
	var one zeroTierNetwork
	if err := json.Unmarshal(out, &one); err != nil {
		return nil, err
	}
	if one.ID == "" && one.Name == "" && one.Status == "" {
		return nil, errors.New("no networks in JSON output")
	}
	return []zeroTierNetwork{one}, nil
}

func parseZeroTierNetworksText(out []byte) ([]zeroTierNetwork, error) {
	var networks []zeroTierNetwork
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "200" || fields[1] != "listnetworks" {
			return nil, fmt.Errorf("unexpected line %q", line)
		}
		statusIndex := zeroTierStatusIndex(fields)
		if statusIndex < 0 {
			return nil, fmt.Errorf("missing status in line %q", line)
		}
		network := zeroTierNetwork{ID: fields[2], Status: fields[statusIndex]}
		nameEnd := statusIndex
		if statusIndex > 3 && strings.Count(fields[statusIndex-1], ":") >= 2 {
			nameEnd = statusIndex - 1
		}
		if nameEnd > 3 {
			network.Name = strings.Join(fields[3:nameEnd], " ")
		}
		if statusIndex+1 < len(fields) {
			network.Type = fields[statusIndex+1]
		}
		if statusIndex+2 < len(fields) {
			network.PortDeviceName = fields[statusIndex+2]
		}
		if statusIndex+3 < len(fields) {
			network.AssignedAddresses = fields[statusIndex+3:]
		}
		networks = append(networks, network)
	}
	if len(networks) == 0 {
		return nil, errors.New("no networks in text output")
	}
	return networks, nil
}

func zeroTierStatusIndex(fields []string) int {
	for i := 3; i < len(fields); i++ {
		switch strings.ToUpper(strings.TrimSpace(fields[i])) {
		case "OK", "ACCESS_DENIED", "REQUESTING_CONFIGURATION", "NOT_FOUND", "PORT_ERROR", "CLIENT_TOO_OLD", "AUTHENTICATION_REQUIRED":
			return i
		}
	}
	return -1
}

func hostFromAssignedAddresses(addresses []string) (string, bool) {
	var ips []net.IP
	for _, raw := range addresses {
		for _, part := range strings.Split(raw, ",") {
			ip := ipFromZeroTierAddress(part)
			if ip == nil || !isRoutableLANHost(ip.String()) {
				continue
			}
			ips = append(ips, ip)
		}
	}
	return preferredPrivateIP(ips)
}

func preferredPrivateIP(ips []net.IP) (string, bool) {
	var ipv4, ipv6 []net.IP
	seen := map[string]bool{}
	for _, ip := range ips {
		if ip == nil || !isRoutableLANHost(ip.String()) {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
			if !seen[ip.String()] {
				seen[ip.String()] = true
				ipv4 = append(ipv4, ip)
			}
			continue
		}
		if !seen[ip.String()] {
			seen[ip.String()] = true
			ipv6 = append(ipv6, ip)
		}
	}
	sort.Slice(ipv4, func(i, j int) bool { return bytes.Compare(ipv4[i].To16(), ipv4[j].To16()) < 0 })
	sort.Slice(ipv6, func(i, j int) bool { return bytes.Compare(ipv6[i].To16(), ipv6[j].To16()) < 0 })
	if len(ipv4) > 0 {
		return ipv4[0].String(), true
	}
	if len(ipv6) > 0 {
		return ipv6[0].String(), true
	}
	return "", false
}

func ipFromZeroTierAddress(raw string) net.IP {
	raw = strings.Trim(strings.TrimSpace(raw), "[],")
	if raw == "" || raw == "-" {
		return nil
	}
	if strings.Contains(raw, "/") {
		ip, _, err := net.ParseCIDR(raw)
		if err == nil {
			return ip
		}
	}
	return net.ParseIP(raw)
}

func (n *zeroTierNetwork) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID                string   `json:"id"`
		NWID              string   `json:"nwid"`
		Name              string   `json:"name"`
		Status            string   `json:"status"`
		Type              string   `json:"type"`
		PortDeviceName    string   `json:"portDeviceName"`
		PortDevice        string   `json:"portDevice"`
		Device            string   `json:"device"`
		AssignedAddresses []string `json:"assignedAddresses"`
		AssignedIPs       []string `json:"assignedIps"`
		IPAssignments     []string `json:"ipAssignments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	n.ID = strings.TrimSpace(raw.ID)
	if n.ID == "" {
		n.ID = strings.TrimSpace(raw.NWID)
	}
	n.Name = strings.TrimSpace(raw.Name)
	n.Status = strings.TrimSpace(raw.Status)
	n.Type = strings.TrimSpace(raw.Type)
	n.PortDeviceName = strings.TrimSpace(raw.PortDeviceName)
	if n.PortDeviceName == "" {
		n.PortDeviceName = strings.TrimSpace(raw.PortDevice)
	}
	if n.PortDeviceName == "" {
		n.PortDeviceName = strings.TrimSpace(raw.Device)
	}
	n.AssignedAddresses = raw.AssignedAddresses
	if len(n.AssignedAddresses) == 0 {
		n.AssignedAddresses = raw.AssignedIPs
	}
	if len(n.AssignedAddresses) == 0 {
		n.AssignedAddresses = raw.IPAssignments
	}
	return nil
}

func (n zeroTierNetwork) ready() bool {
	return strings.EqualFold(strings.TrimSpace(n.Status), "OK")
}

func (n zeroTierNetwork) matches(want string) bool {
	want = strings.TrimSpace(want)
	return strings.EqualFold(n.ID, want) || strings.EqualFold(n.Name, want)
}

func (n zeroTierNetwork) display() string {
	id := strings.TrimSpace(n.ID)
	name := strings.TrimSpace(n.Name)
	switch {
	case id != "" && name != "":
		return id + " (" + name + ")"
	case id != "":
		return id
	case name != "":
		return name
	default:
		return "unknown"
	}
}
