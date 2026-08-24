//go:build unix

package installplatform

import (
	"golang.org/x/sys/unix"
)

func acquireProcessLeaseGuard(path string) (func(), error) {
	file, err := openProcessLeaseGuardFile(path)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
	}, nil
}
