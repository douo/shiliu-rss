//go:build release

package webassets

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var assets embed.FS

func Files() fs.FS {
	sub, err := fs.Sub(assets, "build")
	if err != nil {
		panic(err)
	}
	return sub
}
