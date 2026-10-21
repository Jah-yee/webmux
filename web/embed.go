package web

import (
	"archive/zip"
	"bytes"
	_ "embed"
)

// Assets contains production resources built by pnpm before go build. The ZIP
// filesystem decompresses files on demand instead of expanding the whole app
// at startup. internal/api caches each requested file after its first read.
var Assets = embeddedAssets()

//go:embed dist/embed.zip
var archive []byte

func embeddedAssets() *zip.Reader {
	assets, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		panic("invalid embedded frontend archive: " + err.Error())
	}
	return assets
}
