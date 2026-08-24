//go:build darwin

package installplatform

import (
	"time"

	"golang.org/x/sys/unix"
)

func adminInitReclaimChangeTime(path string) (time.Time, bool) {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return time.Time{}, false
	}
	return time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec).UTC(), true
}
