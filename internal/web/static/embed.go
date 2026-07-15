//go:build !onibi_remote

package static

import (
	"embed"
	"io/fs"
)

//go:embed dist/** fonts/**
var embedded embed.FS

var FS fs.FS = embedded
