//go:build !linux && !darwin && !windows

package installplatform

import "time"

func adminInitReclaimChangeTime(string) (time.Time, bool) {
	return time.Time{}, false
}
