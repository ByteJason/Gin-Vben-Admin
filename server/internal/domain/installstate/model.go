package installstate

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const CurrentSchemaVersion = 1

type UI string

const (
	UIAntd  UI = "antd"
	UIEle   UI = "ele"
	UINaive UI = "naive"
)

type Mode string

const (
	ModeEmbedded   Mode = "embedded"
	ModeStandalone Mode = "standalone"
	ModeAPIOnly    Mode = "api_only"
	ModeDev        Mode = "dev"
)

// Marker is the credential-free, durable installation lock. Runtime secrets
// belong to the configured secret source and must never be added here.
type Marker struct {
	SchemaVersion    int       `json:"schema_version"`
	InstallerVersion string    `json:"installer_version"`
	InstalledAt      time.Time `json:"installed_at"`
	SelectedUI       UI        `json:"selected_ui"`
	Mode             Mode      `json:"mode"`
	ArtifactHash     string    `json:"artifact_hash"`
	ManifestHash     string    `json:"manifest_hash"`
}

func (m Marker) Validate() error {
	if m.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported marker schema version %d", m.SchemaVersion)
	}
	if version := strings.TrimSpace(m.InstallerVersion); version == "" || len(version) > 64 {
		return errors.New("installer version is required and must not exceed 64 characters")
	}
	if m.InstalledAt.IsZero() {
		return errors.New("installation completion time is required")
	}
	switch m.SelectedUI {
	case UIAntd, UIEle, UINaive:
	default:
		return fmt.Errorf("unsupported management UI %q", m.SelectedUI)
	}
	switch m.Mode {
	case ModeEmbedded, ModeStandalone, ModeAPIOnly, ModeDev:
	default:
		return fmt.Errorf("unsupported UI mode %q", m.Mode)
	}
	if err := validateSHA256("artifact", m.ArtifactHash); err != nil {
		return err
	}
	if err := validateSHA256("manifest", m.ManifestHash); err != nil {
		return err
	}
	return nil
}

func validateSHA256(name, value string) error {
	if len(value) != 64 {
		return fmt.Errorf("%s hash must be a SHA-256 digest", name)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s hash must be a SHA-256 digest", name)
	}
	return nil
}
