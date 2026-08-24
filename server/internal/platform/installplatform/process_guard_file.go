package installplatform

import (
	"errors"
	"os"
)

const processGuardOpenAttempts = 3

// openProcessLeaseGuardFile never follows a guard symlink. A missing path is
// created with O_EXCL; an existing path is opened only after and before exact
// inode checks, so a replacement cannot redirect the OS lock to another file.
func openProcessLeaseGuardFile(path string) (*os.File, error) {
	for attempt := 0; attempt < processGuardOpenAttempts; attempt++ {
		before, err := os.Lstat(path)
		missing := errors.Is(err, os.ErrNotExist)
		if err != nil && !missing {
			return nil, errProcessLeaseBusy
		}
		if !missing && !before.Mode().IsRegular() {
			return nil, errProcessLeaseBusy
		}

		flags := os.O_RDWR
		if missing {
			flags |= os.O_CREATE | os.O_EXCL
		}
		file, err := os.OpenFile(path, flags, 0o600)
		if missing && errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		opened, openedErr := file.Stat()
		published, publishedErr := os.Lstat(path)
		valid := openedErr == nil && publishedErr == nil &&
			opened.Mode().IsRegular() && published.Mode().IsRegular() &&
			os.SameFile(opened, published) &&
			(missing || os.SameFile(before, opened))
		if !valid {
			_ = file.Close()
			return nil, errProcessLeaseBusy
		}
		return file, nil
	}
	return nil, errProcessLeaseBusy
}
