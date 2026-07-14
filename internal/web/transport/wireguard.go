package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

const (
	WireGuardBinEnv       = "ONIBI_WIREGUARD_BIN"
	WireGuardInterfaceEnv = "ONIBI_WIREGUARD_INTERFACE"
	wireguardProvider     = "wireguard"
)

type WireGuard struct {
	Bin       string
	Interface string

	runner         commandRunner
	lookPath       func(string) (string, error)
	interfaceAddrs func(string) ([]net.Addr, error)
	mu             sync.Mutex
	port           int
	host           string
	iface          string
}

func NewWireGuardFromEnv() *WireGuard {
	return &WireGuard{
		Bin:            wireGuardBin(),
		Interface:      strings.TrimSpace(os.Getenv(WireGuardInterfaceEnv)),
		runner:         execCommandRunner{},
		lookPath:       exec.LookPath,
		interfaceAddrs: defaultInterfaceAddrs,
	}
}

func wireGuardBin() string {
	if bin := strings.TrimSpace(os.Getenv(WireGuardBinEnv)); bin != "" {
		return bin
	}
	return "wg"
}

func (w *WireGuard) Check(ctx context.Context) error {
	host, iface, err := w.resolveHost(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	activePort := w.port
	activeHost := w.host
	activeIface := w.iface
	w.mu.Unlock()
	if activePort > 0 && (host != activeHost || iface != activeIface) {
		return Diagnostic(DiagActivationLag, wireguardProvider, "active WireGuard endpoint changed", nil)
	}
	return nil
}

func (w *WireGuard) Enable(ctx context.Context, localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port %d", localPort)
	}
	host, iface, err := w.resolveHost(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.port = localPort
	w.host = host
	w.iface = iface
	w.mu.Unlock()
	return nil
}

func (w *WireGuard) URL(ctx context.Context) (string, error) {
	w.mu.Lock()
	host := strings.TrimSpace(w.host)
	port := w.port
	w.mu.Unlock()
	if host == "" {
		var iface string
		var err error
		host, iface, err = w.resolveHost(ctx)
		if err != nil {
			return "", err
		}
		w.mu.Lock()
		w.host = host
		w.iface = iface
		port = w.port
		w.mu.Unlock()
	}
	if port <= 0 || port > 65535 {
		return "", errors.New("wireguard local port not set")
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func (w *WireGuard) Disable(context.Context) error {
	w.mu.Lock()
	w.port = 0
	w.host = ""
	w.iface = ""
	w.mu.Unlock()
	return nil
}

func (w *WireGuard) BindHost(ctx context.Context) (string, error) {
	host, iface, err := w.resolveHost(ctx)
	if err != nil {
		return "", err
	}
	w.mu.Lock()
	w.host = host
	w.iface = iface
	w.mu.Unlock()
	return host, nil
}

func (w *WireGuard) InterfaceName() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimSpace(w.iface)
}

func (w *WireGuard) resolveHost(ctx context.Context) (string, string, error) {
	if err := checkBinary(w.bin(), w.lookPath, wireguardProvider); err != nil {
		return "", "", err
	}
	interfaces, err := w.wireGuardInterfaces(ctx)
	if err != nil {
		return "", "", err
	}
	iface := strings.TrimSpace(w.Interface)
	if iface != "" {
		if !containsString(interfaces, iface) {
			return "", "", fmt.Errorf("wireguard interface %q not reported by wg show interfaces", iface)
		}
		interfaces = []string{iface}
	}
	for _, name := range interfaces {
		host, ok := w.hostForInterface(name)
		if ok {
			return host, name, nil
		}
	}
	if iface != "" {
		return "", "", fmt.Errorf("wireguard interface %q has no routable IP address", iface)
	}
	return "", "", errors.New("no WireGuard interface with a routable IP address")
}

func (w *WireGuard) wireGuardInterfaces(ctx context.Context) ([]string, error) {
	out, err := w.run(ctx, "show", "interfaces")
	if err != nil {
		return nil, fmt.Errorf("wg show interfaces: %w", err)
	}
	var names []string
	seen := map[string]bool{}
	for _, name := range strings.Fields(string(out)) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, errors.New("wg show interfaces returned no WireGuard interfaces")
	}
	return names, nil
}

func (w *WireGuard) hostForInterface(name string) (string, bool) {
	addrs := w.interfaceAddrs
	if addrs == nil {
		addrs = defaultInterfaceAddrs
	}
	got, err := addrs(name)
	if err != nil {
		return "", false
	}
	var ipv6 string
	for _, addr := range got {
		ip := addrIP(addr)
		if ip == nil || !isRoutableLANHost(ip.String()) {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String(), true
		}
		if ipv6 == "" {
			ipv6 = ip.String()
		}
	}
	if ipv6 != "" {
		return ipv6, true
	}
	return "", false
}

func (w *WireGuard) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := w.bin()
	runner := w.runner
	if runner == nil {
		runner = execCommandRunner{}
	}
	return runner.Run(ctx, bin, args...)
}

func (w *WireGuard) bin() string {
	if strings.TrimSpace(w.Bin) == "" {
		return "wg"
	}
	return strings.TrimSpace(w.Bin)
}

func defaultInterfaceAddrs(name string) ([]net.Addr, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, err
	}
	if iface.Flags&net.FlagUp == 0 {
		return nil, fmt.Errorf("interface %s is down", name)
	}
	return iface.Addrs()
}

func containsString(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}
