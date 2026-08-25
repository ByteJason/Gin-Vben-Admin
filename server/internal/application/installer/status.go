package installer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

const CurrentInstallerVersion = "0.4.0-dev"

type State string

const (
	StatePristine     State = "pristine"
	StateUIPrepared   State = "ui_prepared"
	StateInstalling   State = "installing"
	StateInstalled    State = "installed"
	StateInconsistent State = "inconsistent"
	// StateUninstalled is retained only by the legacy marker-only constructor.
	// Profile-aware status responses always use StatePristine instead.
	StateUninstalled State = "uninstalled"
)

type MarkerReader interface {
	Load(context.Context) (installstate.Marker, bool, error)
}

// InstallationProfile is the credential-free UI selection prepared by the
// local initializer. It is read-only from the installation API boundary.
type InstallationProfile struct {
	SelectedUI  installstate.UI     `json:"selectedUi"`
	Installing  bool                `json:"installing"`
	PreparingUI bool                `json:"preparingUi"`
	UIAction    UIPreparationAction `json:"uiAction,omitempty"`
}

// ProfileProvider returns the initializer's selected UI. exists=false means
// no profile has been prepared; Installing denotes an active apply job.
type ProfileProvider interface {
	Profile(context.Context) (profile InstallationProfile, exists bool, err error)
}

// InstallationActivity exposes only whether a first-install job currently
// owns the apply transaction. It deliberately omits job identifiers and input.
type InstallationActivity interface {
	InstallationActive() bool
}

// CompletionReconciler removes only transaction-owned completion artifacts
// left after the marker commit point. Status treats failures as housekeeping
// failures: the installation remains visible and a later attempt retries.
type CompletionReconciler interface {
	ReconcileCompleted(context.Context) error
}

type activityProfileProvider struct {
	profiles ProfileProvider
	activity InstallationActivity
}

// NewActivityProfileProvider decorates a durable UI profile with transient job
// activity for status reporting. Planning keeps using the undecorated profile,
// so a running temporary HTTP process does not make the profile unusable.
func NewActivityProfileProvider(profiles ProfileProvider, activity InstallationActivity) ProfileProvider {
	return activityProfileProvider{profiles: profiles, activity: activity}
}

func (p activityProfileProvider) Profile(ctx context.Context) (InstallationProfile, bool, error) {
	if p.profiles == nil {
		return InstallationProfile{}, false, errors.New("installation profile provider is not configured")
	}
	profile, exists, err := p.profiles.Profile(ctx)
	if err != nil || !exists {
		return profile, exists, err
	}
	profile.Installing = profile.Installing || p.activity != nil && p.activity.InstallationActive()
	return profile, true, nil
}

type InstallationPhase string

const (
	InstallationPhaseUIPrepare InstallationPhase = "ui_prepare"
	InstallationPhaseApply     InstallationPhase = "apply"
)

// Status is the public, credential-free installation summary. Artifact and
// manifest hashes stay in the local marker and are intentionally omitted from
// the unauthenticated status response.
type Status struct {
	State            State               `json:"state"`
	Installed        bool                `json:"installed"`
	SchemaVersion    int                 `json:"schemaVersion"`
	InstallerVersion string              `json:"installerVersion"`
	SelectedUI       installstate.UI     `json:"selectedUi,omitempty"`
	Mode             installstate.Mode   `json:"mode,omitempty"`
	InstalledAt      *time.Time          `json:"installedAt,omitempty"`
	Phase            InstallationPhase   `json:"phase,omitempty"`
	UIAction         UIPreparationAction `json:"uiAction,omitempty"`
}

type StatusService struct {
	markers    MarkerReader
	profiles   ProfileProvider
	reconciler CompletionReconciler
	legacy     bool

	reconcileMu       sync.Mutex
	reconcileComplete bool
}

// NewStatusService keeps marker-only embeddings compatible. New initializer
// runtimes use NewStatusServiceWithProfile for profile/marker consistency.
func NewStatusService(markers MarkerReader) *StatusService {
	return &StatusService{markers: markers, legacy: true}
}

func NewStatusServiceWithProfile(markers MarkerReader, profiles ProfileProvider) *StatusService {
	return &StatusService{markers: markers, profiles: profiles}
}

// NewStatusServiceWithProfileAndReconciler enables bounded completion
// housekeeping from the installed status path. A transient cleanup error is
// retried without hiding the already-committed marker from callers.
func NewStatusServiceWithProfileAndReconciler(markers MarkerReader, profiles ProfileProvider, reconciler CompletionReconciler) *StatusService {
	return &StatusService{markers: markers, profiles: profiles, reconciler: reconciler}
}

func (s *StatusService) Status(ctx context.Context) (Status, error) {
	if s == nil || s.markers == nil {
		return Status{}, errors.New("installation marker reader is not configured")
	}
	marker, markerExists, err := s.markers.Load(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("read installation marker: %w", err)
	}
	if markerExists {
		s.reconcileCompletion(ctx)
	}
	if s.legacy {
		return statusFromLegacyMarker(marker, markerExists)
	}
	if s.profiles == nil {
		return Status{}, errors.New("installation profile provider is not configured")
	}
	profile, profileExists, err := s.profiles.Profile(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("read installation profile: %w", err)
	}
	if !markerExists && !profileExists {
		return pristineStatus(), nil
	}
	if !profileExists || !validProfile(profile) {
		return inconsistentStatus(profile), nil
	}
	if !markerExists {
		state := StateUIPrepared
		if profile.Installing {
			state = StateInstalling
		}
		return Status{State: state, SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: CurrentInstallerVersion, SelectedUI: profile.SelectedUI, Phase: installationPhase(profile), UIAction: installationUIAction(profile)}, nil
	}
	if err := marker.Validate(); err != nil {
		return inconsistentStatus(profile), nil
	}
	if marker.SelectedUI != profile.SelectedUI {
		return inconsistentStatus(profile), nil
	}
	if profile.Installing {
		return Status{State: StateInstalling, SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: CurrentInstallerVersion, SelectedUI: profile.SelectedUI, Phase: installationPhase(profile), UIAction: installationUIAction(profile)}, nil
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

func (s *StatusService) reconcileCompletion(ctx context.Context) {
	if s.reconciler == nil {
		return
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	if s.reconcileComplete {
		return
	}
	const attempts = 3
	for attempt := 0; attempt < attempts; attempt++ {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		if err := s.reconciler.ReconcileCompleted(ctx); err == nil {
			s.reconcileComplete = true
			return
		}
	}
}

func statusFromLegacyMarker(marker installstate.Marker, exists bool) (Status, error) {
	if !exists {
		return Status{State: StateUninstalled, SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: CurrentInstallerVersion}, nil
	}
	if err := marker.Validate(); err != nil {
		return inconsistentStatus(InstallationProfile{}), nil
	}
	installedAt := marker.InstalledAt
	return Status{State: StateInstalled, Installed: true, SchemaVersion: marker.SchemaVersion, InstallerVersion: marker.InstallerVersion, SelectedUI: marker.SelectedUI, Mode: marker.Mode, InstalledAt: &installedAt}, nil
}

func pristineStatus() Status {
	return Status{State: StatePristine, SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: CurrentInstallerVersion}
}

func inconsistentStatus(profile InstallationProfile) Status {
	return Status{State: StateInconsistent, SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: CurrentInstallerVersion, SelectedUI: profile.SelectedUI}
}

func validProfile(profile InstallationProfile) bool {
	switch profile.SelectedUI {
	case installstate.UIAntd, installstate.UIEle, installstate.UINaive:
	default:
		return false
	}
	if profile.PreparingUI {
		return profile.Installing && (profile.UIAction == UIPreparationActionPrepare || profile.UIAction == UIPreparationActionReset)
	}
	return profile.UIAction == ""
}

func installationUIAction(profile InstallationProfile) UIPreparationAction {
	if profile.Installing && profile.PreparingUI {
		return profile.UIAction
	}
	return ""
}

func installationPhase(profile InstallationProfile) InstallationPhase {
	if !profile.Installing {
		return ""
	}
	if profile.PreparingUI {
		return InstallationPhaseUIPrepare
	}
	return InstallationPhaseApply
}
