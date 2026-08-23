package installer

import (
	"context"
	"errors"
	"fmt"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

const CurrentInstallerVersion = "0.4.0-dev"

type State string

const (
	StateUninstalled State = "uninstalled"
	StateInstalled   State = "installed"
)

type MarkerReader interface {
	Load(context.Context) (installstate.Marker, bool, error)
}

// Status is the public, credential-free installation summary. Artifact and
// manifest hashes stay in the local marker and are intentionally omitted from
// the unauthenticated status response.
type Status struct {
	State            State             `json:"state"`
	Installed        bool              `json:"installed"`
	SchemaVersion    int               `json:"schemaVersion"`
	InstallerVersion string            `json:"installerVersion"`
	SelectedUI       installstate.UI   `json:"selectedUi,omitempty"`
	Mode             installstate.Mode `json:"mode,omitempty"`
	InstalledAt      *time.Time        `json:"installedAt,omitempty"`
}

type StatusService struct {
	markers MarkerReader
}

func NewStatusService(markers MarkerReader) *StatusService {
	return &StatusService{markers: markers}
}

func (s *StatusService) Status(ctx context.Context) (Status, error) {
	if s == nil || s.markers == nil {
		return Status{}, errors.New("installation marker reader is not configured")
	}
	marker, installed, err := s.markers.Load(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("read installation marker: %w", err)
	}
	if !installed {
		return Status{
			State:            StateUninstalled,
			Installed:        false,
			SchemaVersion:    installstate.CurrentSchemaVersion,
			InstallerVersion: CurrentInstallerVersion,
		}, nil
	}
	if err := marker.Validate(); err != nil {
		return Status{}, fmt.Errorf("validate installation marker: %w", err)
	}
	installedAt := marker.InstalledAt
	return Status{
		State:            StateInstalled,
		Installed:        true,
		SchemaVersion:    marker.SchemaVersion,
		InstallerVersion: marker.InstallerVersion,
		SelectedUI:       marker.SelectedUI,
		Mode:             marker.Mode,
		InstalledAt:      &installedAt,
	}, nil
}
