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
		t.Fatalf("Status() = %#v, want legacy uninstalled", got)
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

func TestStatusServiceDerivesReadOnlyProfileStates(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		marker    markerReaderStub
		profile   profileProviderStub
		want      State
		installed bool
		ui        installstate.UI
		phase     InstallationPhase
		uiAction  UIPreparationAction
	}{
		{name: "pristine", want: StatePristine},
		{name: "ui prepared", profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true}, want: StateUIPrepared, ui: installstate.UIAntd},
		{name: "applying", profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIEle, Installing: true}, exists: true}, want: StateInstalling, ui: installstate.UIEle, phase: InstallationPhaseApply},
		{name: "preparing ui", profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIEle, Installing: true, PreparingUI: true, UIAction: UIPreparationActionPrepare}, exists: true}, want: StateInstalling, ui: installstate.UIEle, phase: InstallationPhaseUIPrepare, uiAction: UIPreparationActionPrepare},
		{name: "resetting ui", profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIEle, Installing: true, PreparingUI: true, UIAction: UIPreparationActionReset}, exists: true}, want: StateInstalling, ui: installstate.UIEle, phase: InstallationPhaseUIPrepare, uiAction: UIPreparationActionReset},
		{name: "installed", marker: markerReaderStub{marker: validMarker(installstate.UINaive), installed: true}, profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UINaive}, exists: true}, want: StateInstalled, installed: true, ui: installstate.UINaive},
		{name: "installed workspace after local UI switch", marker: markerReaderStub{marker: validMarker(installstate.UINaive), installed: true}, profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd, IndependentUISelection: true}, exists: true}, want: StateInstalled, installed: true, ui: installstate.UIAntd},
		{name: "installed workspace while local UI switch is pending", marker: markerReaderStub{marker: validMarker(installstate.UINaive), installed: true}, profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd, Installing: true, PreparingUI: true, UIAction: UIPreparationActionPrepare, IndependentUISelection: true}, exists: true}, want: StateInstalled, installed: true, ui: installstate.UIAntd, phase: InstallationPhaseUIPrepare, uiAction: UIPreparationActionPrepare},
		{name: "damaged marker", marker: markerReaderStub{installed: true}, profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true}, want: StateInconsistent, ui: installstate.UIAntd},
		{name: "mismatched marker and profile", marker: markerReaderStub{marker: validMarker(installstate.UINaive), installed: true}, profile: profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true}, want: StateInconsistent, ui: installstate.UIAntd},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewStatusServiceWithProfile(testCase.marker, testCase.profile)
			got, err := service.Status(context.Background())
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if got.State != testCase.want || got.Installed != testCase.installed || got.SelectedUI != testCase.ui || got.Phase != testCase.phase || got.UIAction != testCase.uiAction {
				t.Fatalf("Status() = %#v, want state=%q installed=%v ui=%q phase=%q action=%q", got, testCase.want, testCase.installed, testCase.ui, testCase.phase, testCase.uiAction)
			}
		})
	}
}

func TestStatusServiceKeepsAValidPublishedMarkerInstallingUntilTheJobFinishes(t *testing.T) {
	service := NewStatusServiceWithProfile(
		markerReaderStub{marker: validMarker(installstate.UIAntd), installed: true},
		profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd, Installing: true}, exists: true},
	)

	got, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if got.State != StateInstalling || got.Installed || got.SelectedUI != installstate.UIAntd {
		t.Fatalf("Status() = %#v, want transient installing state", got)
	}
}

func TestStatusServiceRetriesCompletionReconciliationUntilHousekeepingSucceeds(t *testing.T) {
	reconciler := &completionReconcilerStub{results: []error{
		errors.New("temporary completion housekeeping failure"),
		nil,
	}}
	service := NewStatusServiceWithProfileAndReconciler(
		markerReaderStub{marker: validMarker(installstate.UIAntd), installed: true},
		profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true},
		reconciler,
	)

	got, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !got.Installed || got.State != StateInstalled {
		t.Fatalf("Status() = %#v, want installed despite housekeeping retry", got)
	}
	if reconciler.calls != 2 {
		t.Fatalf("reconciliation calls = %d, want one automatic retry", reconciler.calls)
	}

	if _, err := service.Status(context.Background()); err != nil {
		t.Fatalf("second Status() error = %v", err)
	}
	if reconciler.calls != 2 {
		t.Fatalf("successful reconciliation ran again: calls=%d", reconciler.calls)
	}
}

func TestActivityProfileProviderDecoratesTheReadOnlyProfile(t *testing.T) {
	base := profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIEle}, exists: true}
	provider := NewActivityProfileProvider(base, activityStub{active: true})

	got, exists, err := provider.Profile(context.Background())
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if !exists || got.SelectedUI != installstate.UIEle || !got.Installing {
		t.Fatalf("Profile() = (%#v, %t), want active ele profile", got, exists)
	}
}

func TestActivityProfileProviderDoesNotClearDurableUIPreparationActivity(t *testing.T) {
	base := profileProviderStub{profile: InstallationProfile{
		SelectedUI:  installstate.UIEle,
		Installing:  true,
		PreparingUI: true,
		UIAction:    UIPreparationActionReset,
	}, exists: true}
	provider := NewActivityProfileProvider(base, activityStub{active: false})

	got, exists, err := provider.Profile(context.Background())
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if !exists || !got.Installing || !got.PreparingUI || got.UIAction != UIPreparationActionReset {
		t.Fatalf("Profile() = (%#v, %t), want durable UI preparation activity preserved", got, exists)
	}
}

func validMarker(ui installstate.UI) installstate.Marker {
	return installstate.Marker{
		SchemaVersion:    installstate.CurrentSchemaVersion,
		InstallerVersion: CurrentInstallerVersion,
		InstalledAt:      time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
		SelectedUI:       ui,
		Mode:             installstate.ModeEmbedded,
		ArtifactHash:     strings.Repeat("a", 64),
		ManifestHash:     strings.Repeat("b", 64),
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

type profileProviderStub struct {
	profile InstallationProfile
	exists  bool
	err     error
}

func (s profileProviderStub) Profile(context.Context) (InstallationProfile, bool, error) {
	return s.profile, s.exists, s.err
}

type activityStub struct{ active bool }

func (s activityStub) InstallationActive() bool { return s.active }

type completionReconcilerStub struct {
	results []error
	calls   int
}

func (s *completionReconcilerStub) ReconcileCompleted(context.Context) error {
	resultIndex := s.calls
	s.calls++
	if resultIndex < len(s.results) {
		return s.results[resultIndex]
	}
	return nil
}
