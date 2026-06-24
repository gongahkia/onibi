package web

import "github.com/gongahkia/onibi/internal/web/transport"

func PreferredHost() string {
	return transport.PreferredHost()
}

func LANHosts() []string {
	ips := transport.DetectLANIPs()
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}
