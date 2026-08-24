//go:build windows

package monitor

import "golang.org/x/sys/windows"

func diskSpace(path string) (total, available int64, err error) {
	directory, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var availableBytes uint64
	var totalBytes uint64
	var freeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(directory, &availableBytes, &totalBytes, &freeBytes); err != nil {
		return 0, 0, err
	}
	return int64(totalBytes), int64(availableBytes), nil
}
