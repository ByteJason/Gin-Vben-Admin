package installplatform

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

var (
	ErrAlreadyInstalled    = errors.New("application is already installed")
	ErrInstallationBusy    = errors.New("installation marker is being written")
	ErrInstallationChanged = errors.New("installation marker belongs to a different transaction")
)

const maxMarkerBytes = 16 << 10

type FileMarkerStore struct {
	path string
}

// Remove deletes a marker only when it is byte-for-byte equivalent to the
// transaction marker supplied by the caller. A missing marker is an idempotent
// success; a different marker is never removed.
func (s *FileMarkerStore) Remove(ctx context.Context, expected installstate.Marker) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.path == "." || s.path == "" {
		return errors.New("installation marker path is required")
	}
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("validate expected installation marker: %w", err)
	}
	if _, err := os.Lstat(s.path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect installation marker: %w", err)
	}

	lockPath := s.path + ".lock"
	release, err := acquireProcessLease(lockPath)
	if errors.Is(err, errProcessLeaseBusy) {
		return ErrInstallationBusy
	}
	if err != nil {
		return fmt.Errorf("acquire installation marker lock: %w", err)
	}
	defer release()

	current, installed, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if !installed {
		return nil
	}
	if current != expected {
		return ErrInstallationChanged
	}
	if err := os.Remove(s.path); err != nil {
		return fmt.Errorf("remove installation marker: %w", err)
	}
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("sync installation state directory: %w", err)
	}
	return nil
}

func NewFileMarkerStore(path string) *FileMarkerStore {
	return &FileMarkerStore{path: filepath.Clean(path)}
}

func (s *FileMarkerStore) Load(ctx context.Context) (installstate.Marker, bool, error) {
	if err := contextError(ctx); err != nil {
		return installstate.Marker{}, false, err
	}
	if s == nil || s.path == "." || s.path == "" {
		return installstate.Marker{}, false, errors.New("installation marker path is required")
	}
	// A crash after the atomic marker rename but before lease release must not
	// keep build/dev gates blocked forever. Live or recent unknown leases remain
	// untouched; only dead/expired leases are reclaimed.
	if _, err := os.Lstat(s.path + ".lock"); err == nil {
		_ = reclaimProcessLease(s.path+".lock", time.Now().UTC())
	}

	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return installstate.Marker{}, false, nil
	}
	if err != nil {
		return installstate.Marker{}, false, fmt.Errorf("inspect installation marker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return installstate.Marker{}, false, errors.New("installation marker must be a regular file")
	}

	file, err := os.Open(s.path)
	if err != nil {
		return installstate.Marker{}, false, fmt.Errorf("open installation marker: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, maxMarkerBytes))
	decoder.DisallowUnknownFields()
	var marker installstate.Marker
	if err := decoder.Decode(&marker); err != nil {
		return installstate.Marker{}, false, errors.New("decode installation marker")
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return installstate.Marker{}, false, err
	}
	if err := marker.Validate(); err != nil {
		return installstate.Marker{}, false, fmt.Errorf("validate installation marker: %w", err)
	}
	return marker, true, nil
}

func (s *FileMarkerStore) Create(ctx context.Context, marker installstate.Marker) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.path == "." || s.path == "" {
		return errors.New("installation marker path is required")
	}
	if err := marker.Validate(); err != nil {
		return fmt.Errorf("validate installation marker: %w", err)
	}
	if _, err := os.Lstat(s.path); err == nil {
		return ErrAlreadyInstalled
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect installation marker: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create installation state directory: %w", err)
	}
	lockPath := s.path + ".lock"
	release, err := acquireProcessLease(lockPath)
	if errors.Is(err, errProcessLeaseBusy) {
		return ErrInstallationBusy
	}
	if err != nil {
		return fmt.Errorf("acquire installation marker lock: %w", err)
	}
	defer release()

	if _, err := os.Lstat(s.path); err == nil {
		return ErrAlreadyInstalled
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("recheck installation marker: %w", err)
	}

	encoded, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return errors.New("encode installation marker")
	}
	encoded = append(encoded, '\n')

	temp, err := os.CreateTemp(dir, ".installed.tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary installation marker: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set installation marker permissions: %w", err)
	}
	writer := bufio.NewWriter(temp)
	if _, err := writer.Write(encoded); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write installation marker: %w", err)
	}
	if err := writer.Flush(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("flush installation marker: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync installation marker: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close installation marker: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("publish installation marker: %w", err)
	}
	removeTemp = false
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync installation state directory: %w", err)
	}
	return nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("installation marker contains trailing data")
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func syncDirectory(path string) error {
	return syncDirectoryForPlatform(path, runtime.GOOS)
}

func syncDirectoryForPlatform(path, goos string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("sync path is not a directory")
	}
	// Windows does not support FlushFileBuffers on directory handles. Atomic
	// file replacement still provides the commit boundary there, so validating
	// the directory is the portable best-effort equivalent.
	if goos == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
