//go:build linux

package monitor

import (
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
)

func processRSS() (int64, string, error) {
	payload, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, "", err
	}
	fields := strings.Fields(string(payload))
	if len(fields) < 2 {
		return 0, "", errors.New("process statm is incomplete")
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, "", errors.New("process statm RSS is invalid")
	}
	pageSize := uint64(os.Getpagesize())
	if pageSize == 0 || pages > math.MaxInt64/pageSize {
		return 0, "", errors.New("process RSS is out of range")
	}
	return int64(pages * pageSize), "proc.self.statm.rss", nil
}
