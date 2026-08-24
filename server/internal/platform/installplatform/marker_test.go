package installplatform_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
)

func TestFileMarkerStoreCreatesAndLoadsInstallationMarker(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "install", ".installed")
	store := installplatform.NewFileMarkerStore(path)
	marker := installstate.Marker{
		SchemaVersion:    1,
		InstallerVersion: "0.4.0-dev",
		InstalledAt:      time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC),
		SelectedUI:       installstate.UIAntd,
		Mode:             installstate.ModeEmbedded,
		ArtifactHash:     strings.Repeat("a", 64),
		ManifestHash:     strings.Repeat("b", 64),
	}

	if err := store.Create(context.Background(), marker); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, installed, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !installed {
		t.Fatal("Load() installed = false, want true")
	}
	if got != marker {
		t.Fatalf("Load() marker = %#v, want %#v", got, marker)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("marker mode = %o, want 600", info.Mode().Perm())
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	for _, forbidden := range []string{"password", "secret", "dsn", "token"} {
		if strings.Contains(strings.ToLower(string(contents)), forbidden) {
			t.Fatalf("marker contains forbidden credential field %q: %s", forbidden, contents)
		}
	}
}

func TestFileMarkerStoreRejectsRepeatedInstallation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "install", ".installed")
	store := installplatform.NewFileMarkerStore(path)
	marker := installstate.Marker{
		SchemaVersion:    1,
		InstallerVersion: "0.4.0-dev",
		InstalledAt:      time.Now().UTC().Truncate(time.Second),
		SelectedUI:       installstate.UIEle,
		Mode:             installstate.ModeStandalone,
		ArtifactHash:     strings.Repeat("c", 64),
		ManifestHash:     strings.Repeat("d", 64),
	}

	if err := store.Create(context.Background(), marker); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if err := store.Create(context.Background(), marker); !errors.Is(err, installplatform.ErrAlreadyInstalled) {
		t.Fatalf("second Create() error = %v, want ErrAlreadyInstalled", err)
	}
}

func TestFileMarkerStoreReportsUninstalledWithoutCreatingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "install", ".installed")
	store := installplatform.NewFileMarkerStore(path)

	marker, installed, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if installed || marker != (installstate.Marker{}) {
		t.Fatalf("Load() = (%#v, %v), want zero marker and false", marker, installed)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() created state directory or returned unexpected error: %v", err)
	}
}

func TestFileMarkerStoreReclaimsStaleLegacyLockAfterInterruptedProcess(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "install", ".installed")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	store := installplatform.NewFileMarkerStore(path)
	if err := store.Create(context.Background(), validTestMarker()); err != nil {
		t.Fatalf("Create() with stale legacy lock error = %v", err)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("stale marker lock remains: %v", err)
	}
}

func TestFileMarkerStoreKeepsRecentUnknownLockBusy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "install", ".installed")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store := installplatform.NewFileMarkerStore(path)
	if err := store.Create(context.Background(), validTestMarker()); !errors.Is(err, installplatform.ErrInstallationBusy) {
		t.Fatalf("Create() with recent unknown lock error = %v, want ErrInstallationBusy", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("recent lock was removed: %v", err)
	}
}

func TestFileMarkerStoreLoadReclaimsStalePostPublishLock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "install", ".installed")
	store := installplatform.NewFileMarkerStore(path)
	if err := store.Create(context.Background(), validTestMarker()); err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	if _, installed, err := store.Load(context.Background()); err != nil || !installed {
		t.Fatalf("Load() = installed:%t error:%v", installed, err)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("stale post-publish marker lock remains: %v", err)
	}
}

func validTestMarker() installstate.Marker {
	return installstate.Marker{
		SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: "0.4.0-dev",
		InstalledAt: time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
		SelectedUI:  installstate.UIAntd, Mode: installstate.ModeDev,
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	}
}

func TestFileMarkerStoreRemovesOnlyTheExpectedInstallation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "install", ".installed")
	store := installplatform.NewFileMarkerStore(path)
	marker := installstate.Marker{
		SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: "0.4.0-dev",
		InstalledAt: time.Date(2026, time.August, 21, 15, 0, 0, 0, time.UTC),
		SelectedUI:  installstate.UIAntd, Mode: installstate.ModeEmbedded,
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	}
	if err := store.Create(context.Background(), marker); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	different := marker
	different.ManifestHash = strings.Repeat("c", 64)
	if err := store.Remove(context.Background(), different); !errors.Is(err, installplatform.ErrInstallationChanged) {
		t.Fatalf("Remove(different) error = %v, want ErrInstallationChanged", err)
	}
	if _, installed, err := store.Load(context.Background()); err != nil || !installed {
		t.Fatalf("different marker was removed: installed=%v error=%v", installed, err)
	}
	if err := store.Remove(context.Background(), marker); err != nil {
		t.Fatalf("Remove(expected) error = %v", err)
	}
	if _, installed, err := store.Load(context.Background()); err != nil || installed {
		t.Fatalf("expected marker still exists: installed=%v error=%v", installed, err)
	}
	if err := store.Remove(context.Background(), marker); err != nil {
		t.Fatalf("Remove(missing) error = %v, want idempotent nil", err)
	}
}
