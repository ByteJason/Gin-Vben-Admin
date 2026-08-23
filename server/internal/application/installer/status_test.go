package installer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestStatusServiceReportsUninstalledState(t *testing.T) {
	service := NewStatusService(markerReaderStub{})

	got, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if got.Installed || got.State != StateUninstalled {
		t.Fatalf("Status() = %#v, want uninstalled", got)
	}
	if got.SchemaVersion != installstate.CurrentSchemaVersion || got.InstallerVersion != CurrentInstallerVersion {
		t.Fatalf("Status() version metadata = %#v", got)
	}
	if got.SelectedUI != "" || got.Mode != "" || got.InstalledAt != nil {
		t.Fatalf("uninstalled Status() leaked marker fields: %#v", got)
	}
}

func TestStatusServiceReportsCredentialFreeInstalledSummary(t *testing.T) {
	installedAt := time.Date(2026, time.August, 21, 10, 0, 0, 0, time.UTC)
	marker := installstate.Marker{
		SchemaVersion:    installstate.CurrentSchemaVersion,
		InstallerVersion: "0.4.0-dev",
		InstalledAt:      installedAt,
		SelectedUI:       installstate.UINaive,
		Mode:             installstate.ModeEmbedded,
		ArtifactHash:     strings.Repeat("a", 64),
		ManifestHash:     strings.Repeat("b", 64),
	}
	service := NewStatusService(markerReaderStub{marker: marker, installed: true})

	got, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !got.Installed || got.State != StateInstalled {
		t.Fatalf("Status() = %#v, want installed", got)
	}
	if got.SelectedUI != installstate.UINaive || got.Mode != installstate.ModeEmbedded {
		t.Fatalf("Status() selection = %#v", got)
	}
	if got.InstalledAt == nil || !got.InstalledAt.Equal(installedAt) {
		t.Fatalf("Status() installedAt = %v, want %v", got.InstalledAt, installedAt)
	}
}

func TestStatusServiceDoesNotHideMarkerReadFailures(t *testing.T) {
	want := errors.New("corrupt marker fixture")
	service := NewStatusService(markerReaderStub{err: want})

	_, err := service.Status(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Status() error = %v, want wrapped marker error", err)
	}
}

type markerReaderStub struct {
	marker    installstate.Marker
	installed bool
	err       error
}

func (s markerReaderStub) Load(context.Context) (installstate.Marker, bool, error) {
	return s.marker, s.installed, s.err
}
