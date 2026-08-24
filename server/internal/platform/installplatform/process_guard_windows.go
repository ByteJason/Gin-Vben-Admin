//go:build windows

package installplatform

import (
	"golang.org/x/sys/windows"
)

func acquireProcessLeaseGuard(path string) (func(), error) {
	file, err := openProcessLeaseGuardFile(path)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
		_ = file.Close()
	}, nil
}
