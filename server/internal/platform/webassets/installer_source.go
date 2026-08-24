package webassets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const installerSourceDirectory = "admin/apps/install/src"

// InstallerSource exposes the repository's dependency-free installer sources
// using the same install/ subtree shape as a generated frontend bundle.
func InstallerSource(workspaceRoot string) (fs.FS, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return nil, errors.New("installer workspace root is required")
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, errors.New("resolve installer workspace root")
	}
	source, err := installerSourcePath(root)
	if err != nil {
		return nil, errors.New("installer source directory is unavailable")
	}
	if err := filepath.WalkDir(source, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("installer source contains a symbolic link")
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return errors.New("installer source contains an unsupported entry")
		}
		return nil
	}); err != nil {
		return nil, errors.New("installer source directory is invalid")
	}
	for _, name := range []string{"index.html", "app.js", "styles.css"} {
		entry, statErr := os.Lstat(filepath.Join(source, name))
		if statErr != nil || !entry.Mode().IsRegular() || entry.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("installer source entry is unavailable")
		}
	}
	return installerSourceAssets{direct: os.DirFS(source)}, nil
}

func installerSourcePath(workspaceRoot string) (string, error) {
	current := workspaceRoot
	info, err := os.Lstat(current)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fs.ErrNotExist
	}
	for _, segment := range strings.Split(installerSourceDirectory, "/") {
		current = filepath.Join(current, segment)
		info, err = os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fs.ErrNotExist
		}
	}
	return current, nil
}

type installerSourceAssets struct {
	direct fs.FS
}

func (a installerSourceAssets) Open(name string) (fs.File, error) {
	if child, ok := strings.CutPrefix(name, "install/"); ok && publicInstallerEntry(child) {
		return a.direct.Open(child)
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (a installerSourceAssets) Sub(directory string) (fs.FS, error) {
	if directory == "install" {
		return installerSourcePublic{direct: a.direct}, nil
	}
	return nil, fmt.Errorf("%w: %s", fs.ErrNotExist, directory)
}

type installerSourcePublic struct {
	direct fs.FS
}

func (a installerSourcePublic) Open(name string) (fs.File, error) {
	if publicInstallerEntry(name) {
		return a.direct.Open(name)
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func publicInstallerEntry(name string) bool {
	switch name {
	case "index.html", "app.js", "styles.css":
		return true
	default:
		return false
	}
}
