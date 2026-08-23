package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type installerAssets struct {
	direct fs.FS
}

func loadInstallerAssets(directory string) (fs.FS, error) {
	clean := filepath.Clean(strings.TrimSpace(directory))
	if clean == "." || clean == "" {
		return nil, errors.New("installer asset directory is required")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("installer asset directory is unavailable")
	}
	if err := filepath.WalkDir(clean, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("installer assets contain a symbolic link")
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return errors.New("installer assets contain an unsupported entry")
		}
		return nil
	}); err != nil {
		return nil, errors.New("installer asset directory is invalid")
	}
	index, err := os.Lstat(filepath.Join(clean, "index.html"))
	if err != nil || !index.Mode().IsRegular() || index.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("installer index is unavailable")
	}
	return installerAssets{direct: os.DirFS(clean)}, nil
}

func (a installerAssets) Open(name string) (fs.File, error) {
	if name == "install" {
		return a.direct.Open(".")
	}
	if child, ok := strings.CutPrefix(name, "install/"); ok {
		return a.direct.Open(child)
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (a installerAssets) Sub(directory string) (fs.FS, error) {
	if directory == "install" {
		return a.direct, nil
	}
	return nil, fmt.Errorf("%w: %s", fs.ErrNotExist, directory)
}
