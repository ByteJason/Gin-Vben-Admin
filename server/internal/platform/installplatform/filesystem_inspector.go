package installplatform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
)

var (
	ErrWorkspaceRootRequired = errors.New("installation workspace root is required")
	ErrWorkspaceRootInvalid  = errors.New("installation workspace root is invalid")
	ErrPathNotAllowlisted    = errors.New("installation path is not allowlisted")
	ErrSymlinkNotAllowed     = errors.New("installation path contains a symlink")
	ErrPathTraversal         = errors.New("installation path traversal is not allowed")
)

var probeSequence uint64

// FileSystemInspector performs non-destructive capability checks for the small
// allowlist used by the installer. It never removes or renames a project path;
// temporary sentinels are created only in an already-existing parent and are
// removed before Inspect returns.
type FileSystemInspector struct {
	root string
}

// NewFileSystemInspector constructs an inspector rooted at an existing
// workspace. The root is canonicalized once so a symlinked workspace cannot
// make a later relative path escape the intended tree.
func NewFileSystemInspector(root string) (*FileSystemInspector, error) {
	if strings.TrimSpace(root) == "" {
		return nil, ErrWorkspaceRootRequired
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, ErrWorkspaceRootInvalid
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, ErrWorkspaceRootInvalid
	}
	return &FileSystemInspector{root: filepath.Clean(canonical)}, nil
}

// Inspect returns capability information without exposing absolute paths.
// Relative paths are intentionally exact allowlist entries; the installer
// never accepts an arbitrary browser-supplied filesystem path.
func (i *FileSystemInspector) Inspect(ctx context.Context, relative string) (installer.PathPermission, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.PathPermission{}, err
	}
	if i == nil || i.root == "" {
		return installer.PathPermission{}, ErrWorkspaceRootInvalid
	}
	clean, err := normalizeInstallPath(relative)
	if err != nil {
		return installer.PathPermission{}, err
	}
	full, info, parent, err := i.target(ctx, clean)
	if err != nil {
		return installer.PathPermission{}, err
	}
	permission := installer.PathPermission{}
	if info != nil {
		permission.CanRead = readable(full, info.IsDir())
	} else {
		permission.CanRead = readable(parent, true)
	}
	probe, reasons := probeParent(ctx, parent)
	permission.CanWrite = probe.canWrite
	permission.CanCreate = probe.canCreate
	permission.CanRename = probe.canRename
	permission.CanDelete = probe.canDelete
	permission.Reasons = append(permission.Reasons, reasons...)
	if !permission.CanRead {
		permission.Reasons = appendReason(permission.Reasons, "read_not_available")
	}
	return permission, nil
}

func normalizeInstallPath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ErrPathNotAllowlisted
	}
	// Requests use slash-separated names on every platform. Treat a backslash
	// as a separator too, preventing Windows-style traversal on Unix hosts.
	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	if strings.HasPrefix(normalized, "/") || filepath.VolumeName(filepath.FromSlash(normalized)) != "" {
		return "", ErrPathNotAllowlisted
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == ".." {
			return "", ErrPathTraversal
		}
	}
	clean := path.Clean(normalized)
	if clean == "." || !allowlistedInstallPaths[clean] {
		return "", ErrPathNotAllowlisted
	}
	return clean, nil
}

var allowlistedInstallPaths = map[string]bool{
	"install":              true,
	"admin/apps/web-antd":  true,
	"admin/apps/web-ele":   true,
	"admin/apps/web-naive": true,
	"admin/apps/web":       true,
	".env":                 true,
}

func (i *FileSystemInspector) target(ctx context.Context, clean string) (string, os.FileInfo, string, error) {
	full := filepath.Join(i.root, filepath.FromSlash(clean))
	if !withinRoot(i.root, full) {
		return "", nil, "", ErrPathTraversal
	}
	current := i.root
	parts := strings.Split(clean, "/")
	var info os.FileInfo
	for index, component := range parts {
		if err := ctx.Err(); err != nil {
			return "", nil, "", err
		}
		next := filepath.Join(current, component)
		entry, err := os.Lstat(next)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// A missing final target is valid for create/write planning, but
				// every ancestor needed by the operation must already exist.
				if index != len(parts)-1 {
					return "", nil, "", fmt.Errorf("%w: parent_missing", ErrWorkspaceRootInvalid)
				}
				return full, nil, current, nil
			}
			return "", nil, "", fmt.Errorf("%w: path_unavailable", ErrWorkspaceRootInvalid)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return "", nil, "", ErrSymlinkNotAllowed
		}
		if !entry.IsDir() && index < len(parts)-1 {
			return "", nil, "", fmt.Errorf("%w: parent_not_directory", ErrWorkspaceRootInvalid)
		}
		current = next
		info = entry
	}
	return full, info, filepath.Dir(full), nil
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return false
	}
	return !filepath.IsAbs(relative)
}

func readable(name string, directory bool) bool {
	file, err := os.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	if directory {
		_, err = file.Readdirnames(1)
		return err == nil || errors.Is(err, io.EOF)
	}
	return true
}

type probeResult struct {
	canWrite  bool
	canCreate bool
	canRename bool
	canDelete bool
}

func probeParent(ctx context.Context, parent string) (probeResult, []string) {
	result := probeResult{}
	reasons := make([]string, 0, 4)
	if err := ctx.Err(); err != nil {
		return result, []string{"probe_canceled"}
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return result, []string{"parent_not_writable"}
	}
	sequence := atomic.AddUint64(&probeSequence, 1)
	base := fmt.Sprintf(".gin-vben-install-probe-%d-%d-%d", os.Getpid(), time.Now().UnixNano(), sequence)
	created := filepath.Join(parent, base)
	renamed := filepath.Join(parent, base+".renamed")
	cleanup := func() {
		_ = os.Remove(created)
		_ = os.Remove(renamed)
	}
	defer cleanup()
	file, err := os.OpenFile(created, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return result, []string{"create_not_available"}
	}
	result.canCreate = true
	result.canWrite = true
	if _, err := file.Write([]byte("p")); err != nil {
		result.canWrite = false
		reasons = append(reasons, "write_not_available")
	}
	if err := file.Close(); err != nil {
		result.canWrite = false
		reasons = append(reasons, "write_not_available")
	}
	if err := ctx.Err(); err != nil {
		return result, append(reasons, "probe_canceled")
	}
	if err := os.Rename(created, renamed); err != nil {
		reasons = append(reasons, "rename_not_available")
	} else {
		result.canRename = true
	}
	if err := os.Remove(renamed); err != nil {
		reasons = append(reasons, "delete_not_available")
	} else {
		result.canDelete = true
	}
	return result, uniqueReasons(reasons)
}

func appendReason(reasons []string, reason string) []string {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func uniqueReasons(reasons []string) []string {
	result := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		result = appendReason(result, reason)
	}
	return result
}
