//go:build windows

package monitor

import (
	"errors"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

type processMemoryCounters struct {
	Size                       uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func processRSS() (int64, string, error) {
	var counters processMemoryCounters
	counters.Size = uint32(unsafe.Sizeof(counters))
	result, _, callErr := getProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Size),
	)
	if result == 0 {
		if callErr != nil {
			return 0, "", callErr
		}
		return 0, "", errors.New("process memory query failed")
	}
	if uint64(counters.WorkingSetSize) > math.MaxInt64 {
		return 0, "", errors.New("process RSS is out of range")
	}
	return int64(counters.WorkingSetSize), "windows.process.working_set", nil
}
