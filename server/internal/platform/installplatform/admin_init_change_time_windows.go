//go:build windows

package installplatform

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type adminInitWindowsFileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              uint32
}

func adminInitReclaimChangeTime(path string) (time.Time, bool) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return time.Time{}, false
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(handle)
	var info adminInitWindowsFileBasicInfo
	if err := windows.GetFileInformationByHandleEx(
		handle,
		windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil || info.ChangeTime <= 0 {
		return time.Time{}, false
	}
	change := uint64(info.ChangeTime)
	filetime := windows.Filetime{LowDateTime: uint32(change), HighDateTime: uint32(change >> 32)}
	return time.Unix(0, filetime.Nanoseconds()).UTC(), true
}
