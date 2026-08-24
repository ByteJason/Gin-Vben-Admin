//go:build !unix && !windows

package monitor

import "errors"

func diskSpace(string) (total, available int64, err error) {
	return 0, 0, errors.New("disk metrics are unavailable on this platform")
}
