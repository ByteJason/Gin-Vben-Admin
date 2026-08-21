//go:build embed

package webassets

import (
	"embed"
	"io/fs"
)

// generated contains build outputs created by the root build orchestrator.
//
//go:embed dist
var generated embed.FS

// Static returns the immutable frontend bundle compiled into the server.
func Static() (fs.FS, bool) {
	assets, err := fs.Sub(generated, "dist")
	if err != nil {
		return nil, false
	}
	return assets, true
}
