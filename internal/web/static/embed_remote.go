//go:build onibi_remote

package static

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"io/fs"
)

//go:embed dist-remote.zip
var remoteArchive []byte

var FS fs.FS = mustOpen(remoteArchive)

func mustOpen(data []byte) fs.FS {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		panic(err)
	}
	return archive
}
