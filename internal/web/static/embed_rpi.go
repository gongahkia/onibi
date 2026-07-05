//go:build onibi_rpi

package static

import "embed"

//go:embed dist/**
var FS embed.FS
