package web

import "github.com/gongahkia/onibi/internal/web/transport"

func PreferredHost() string {
	return transport.PreferredHost()
}
