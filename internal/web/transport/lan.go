package transport

import (
	"net"
	"sort"
)

func DetectLANIPs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	type candidate struct {
		ip    net.IP
		name  string
		score int
	}
	var out []candidate
	seen := map[string]bool{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addrIP(addr)
			if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
			}
			key := ip.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, candidate{ip: append(net.IP(nil), ip...), name: iface.Name, score: ifaceScore(iface.Name, ip)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score < out[j].score
		}
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return bytesCompare(out[i].ip, out[j].ip) < 0
	})
	ips := make([]net.IP, 0, len(out))
	for _, c := range out {
		ips = append(ips, c.ip)
	}
	return ips
}

func addrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func ifaceScore(name string, ip net.IP) int {
	score := 100
	switch name {
	case "en0", "eth0", "wlan0":
		score = 0
	}
	if ip.To4() == nil {
		score += 10
	}
	return score
}

func bytesCompare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
