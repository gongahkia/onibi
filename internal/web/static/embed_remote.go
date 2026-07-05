//go:build onibi_remote

package static

import "embed"

//go:embed dist/**
var FS embed.FS
