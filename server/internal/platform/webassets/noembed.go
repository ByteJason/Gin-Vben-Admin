//go:build !embed

package webassets

import "io/fs"

// Static reports that an ordinary API build contains no frontend assets.
func Static() (fs.FS, bool) {
	return nil, false
}
