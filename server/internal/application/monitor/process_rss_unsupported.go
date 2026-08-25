//go:build !linux && !windows

package monitor

import "errors"

func processRSS() (int64, string, error) {
	return 0, "", errors.New("current process RSS is unsupported on this platform")
}
