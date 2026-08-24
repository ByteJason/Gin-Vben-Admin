//go:build windows

package installplatform

import (
	"strconv"

	"golang.org/x/sys/windows"
)

func processStartIdentitySupported() bool { return true }

func processStartToken(pid int) (string, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(handle)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", false
	}
	value := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	return processStartTokenDigest("windows\x00" + strconv.FormatUint(value, 10)), true
}
