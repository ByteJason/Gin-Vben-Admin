package installer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestUIPreparationJobServiceRunsPrepareAsynchronouslyWithServiceContext(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(
		runner,
		profileProviderStub{},
		markerReaderStub{},
	)
	t.Cleanup(func() { _ = service.Close() })

	requestContext, cancelRequest := context.WithCancel(context.Background())
	job, err := service.StartPrepare(requestContext, UIPrepareRequest{
		SelectedUI:     installstate.UIEle,
		ConfirmCleanup: true,
	})
	if err != nil {
		t.Fatalf("StartPrepare() error = %v", err)
	}
	if job.ID == "" || job.Action != UIPreparationActionPrepare || job.State != UIPreparationJobQueued ||
		job.SelectedUI != installstate.UIEle || job.CurrentStep != "queued" || job.Progress != 0 || job.LastUpdated.IsZero() {
		t.Fatalf("StartPrepare() = %#v, want queued ele preparation", job)
	}
	cancelRequest()

	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("prepare runner did not start")
	}
	runner.report(UIPreparationProgress{CurrentStep: "dependencies", Progress: 60})
	eventuallyUIPreparationJob(t, service, job.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobRunning && got.CurrentStep == "dependencies" && got.Progress == 60
	})
	runner.release(nil)

	got := eventuallyUIPreparationJob(t, service, job.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobSucceeded
	})
	if got.CurrentStep != "complete" || got.Progress != 100 || got.ErrorKey != "" || got.LogPath != "" {
		t.Fatalf("completed Progress() = %#v", got)
	}
	if runner.prepareUI() != installstate.UIEle {
		t.Fatalf("Prepare() ui = %q, want ele", runner.prepareUI())
	}
}

func TestUIPreparationJobServiceMakesSameSelectionIdempotentAndRejectsConflicts(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	profiles := &mutableProfileProvider{}
	service := NewUIPreparationJobService(runner, profiles, markerReaderStub{})
	t.Cleanup(func() { _ = service.Close() })

	first, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true})
	if err != nil {
		t.Fatalf("first StartPrepare() error = %v", err)
	}
	second, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true})
	if err != nil || second.ID != first.ID {
		t.Fatalf("idempotent StartPrepare() = (%#v, %v), want job %q", second, err, first.ID)
	}
	if _, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UINaive, ConfirmCleanup: true}); !errors.Is(err, ErrUIPreparationConflict) {
		t.Fatalf("different-ui StartPrepare() error = %v, want ErrUIPreparationConflict", err)
	}
	if _, err := service.StartReset(context.Background(), UIResetRequest{ConfirmReset: true}); !errors.Is(err, ErrUIPreparationConflict) {
		t.Fatalf("StartReset() during prepare error = %v, want ErrUIPreparationConflict", err)
	}

	runner.release(nil)
	eventuallyUIPreparationJob(t, service, first.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobSucceeded
	})
	profiles.set(InstallationProfile{SelectedUI: installstate.UIAntd}, true)
	if calls := runner.prepareCalls(); calls != 1 {
		t.Fatalf("Prepare() calls = %d, want 1", calls)
	}
	completedAgain, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true})
	if err != nil || completedAgain.ID != first.ID || completedAgain.State != UIPreparationJobSucceeded {
		t.Fatalf("completed idempotent StartPrepare() = (%#v, %v), want succeeded job %q", completedAgain, err, first.ID)
	}
}

func TestUIPreparationJobServiceDoesNotReuseSucceededPrepareAfterReset(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	profiles := &mutableProfileProvider{}
	service := NewUIPreparationJobService(runner, profiles, markerReaderStub{})
	t.Cleanup(func() { _ = service.Close() })

	prepared, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIEle, ConfirmCleanup: true})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	runner.release(nil)
	eventuallyUIPreparationJob(t, service, prepared.ID, func(got UIPreparationJob) bool { return got.State == UIPreparationJobSucceeded })
	profiles.set(InstallationProfile{SelectedUI: installstate.UIEle}, true)

	reset, err := service.StartReset(context.Background(), UIResetRequest{ConfirmReset: true})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	runner.release(nil)
	eventuallyUIPreparationJob(t, service, reset.ID, func(got UIPreparationJob) bool { return got.State == UIPreparationJobSucceeded })
	profiles.set(InstallationProfile{}, false)

	restarted, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIEle, ConfirmCleanup: true})
	if err != nil {
		t.Fatalf("StartPrepare() after reset error = %v", err)
	}
	if restarted.ID == prepared.ID || restarted.State != UIPreparationJobQueued {
		t.Fatalf("StartPrepare() after reset = %#v, want new queued job distinct from %q", restarted, prepared.ID)
	}
	runner.release(nil)
}

func TestUIPreparationJobServiceInstalledMarkerWinsOverSucceededPrepareCache(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	profiles := &mutableProfileProvider{}
	markers := &mutableMarkerReader{}
	service := NewUIPreparationJobService(runner, profiles, markers)
	t.Cleanup(func() { _ = service.Close() })

	prepared, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true})
	if err != nil {
		t.Fatal(err)
	}
	<-runner.started
	runner.release(nil)
	eventuallyUIPreparationJob(t, service, prepared.ID, func(got UIPreparationJob) bool { return got.State == UIPreparationJobSucceeded })
	profiles.set(InstallationProfile{SelectedUI: installstate.UIAntd}, true)
	markers.set(validMarker(installstate.UIAntd), true)

	if _, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true}); !errors.Is(err, ErrUIPreparationInstalled) {
		t.Fatalf("StartPrepare() after marker commit error = %v, want ErrUIPreparationInstalled", err)
	}
}

func TestUIPreparationJobServiceRetriesSameUIAfterFailedJob(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(runner, profileProviderStub{}, markerReaderStub{})
	t.Cleanup(func() { _ = service.Close() })

	failed, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIEle, ConfirmCleanup: true})
	if err != nil {
		t.Fatalf("first StartPrepare() error = %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("first prepare runner did not start")
	}
	runner.release(errors.New("fixture failure"))
	eventuallyUIPreparationJob(t, service, failed.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobFailed
	})

	retry, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIEle, ConfirmCleanup: true})
	if err != nil {
		t.Fatalf("retry StartPrepare() error = %v", err)
	}
	if retry.ID == failed.ID || retry.State != UIPreparationJobQueued {
		t.Fatalf("retry StartPrepare() = %#v, want a new queued job after %q", retry, failed.ID)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("retry prepare runner did not start")
	}
	runner.release(nil)
	eventuallyUIPreparationJob(t, service, retry.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobSucceeded
	})
	if calls := runner.prepareCalls(); calls != 2 {
		t.Fatalf("Prepare() calls = %d, want 2", calls)
	}
}

func TestUIPreparationJobServiceValidatesRequestsAndDurableState(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		request UIPrepareRequest
		profile profileProviderStub
		marker  markerReaderStub
		wantErr error
	}{
		{name: "missing confirmation", request: UIPrepareRequest{SelectedUI: installstate.UIAntd}, wantErr: ErrUIPreparationInvalid},
		{name: "unknown ui", request: UIPrepareRequest{SelectedUI: "unknown", ConfirmCleanup: true}, wantErr: ErrUIPreparationInvalid},
		{name: "empty ui", request: UIPrepareRequest{ConfirmCleanup: true}, wantErr: ErrUIPreparationInvalid},
		{name: "installed", request: UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true}, marker: markerReaderStub{installed: true}, wantErr: ErrUIPreparationInstalled},
		{name: "different durable ui", request: UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true}, profile: profileProviderStub{exists: true, profile: InstallationProfile{SelectedUI: installstate.UIEle}}, wantErr: ErrUIPreparationConflict},
		{name: "inconsistent durable profile", request: UIPrepareRequest{SelectedUI: installstate.UIAntd, ConfirmCleanup: true}, profile: profileProviderStub{exists: true}, wantErr: ErrUIPreparationConflict},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runner := newControllableUIPreparationRunner()
			service := NewUIPreparationJobService(runner, testCase.profile, testCase.marker)
			t.Cleanup(func() { _ = service.Close() })
			if _, err := service.StartPrepare(context.Background(), testCase.request); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("StartPrepare() error = %v, want %v", err, testCase.wantErr)
			}
			if runner.prepareCalls() != 0 {
				t.Fatal("invalid request invoked the runner")
			}
		})
	}
}

func TestUIPreparationJobServiceReturnsSucceededJobForAlreadyPreparedSameUI(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(
		runner,
		profileProviderStub{exists: true, profile: InstallationProfile{SelectedUI: installstate.UINaive}},
		markerReaderStub{},
	)
	t.Cleanup(func() { _ = service.Close() })

	job, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UINaive, ConfirmCleanup: true})
	if err != nil {
		t.Fatalf("StartPrepare() error = %v", err)
	}
	if job.State != UIPreparationJobSucceeded || job.CurrentStep != "complete" || job.Progress != 100 || job.SelectedUI != installstate.UINaive {
		t.Fatalf("StartPrepare() = %#v, want idempotent succeeded job", job)
	}
	if runner.prepareCalls() != 0 {
		t.Fatal("already prepared UI invoked the runner")
	}
}

func TestUIPreparationJobServiceRevalidatesIndependentWorkspaceSelection(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(
		runner,
		profileProviderStub{exists: true, profile: InstallationProfile{
			SelectedUI: installstate.UIEle, IndependentUISelection: true,
		}},
		markerReaderStub{},
	)
	t.Cleanup(func() { _ = service.Close() })

	job, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIEle, ConfirmCleanup: true})
	if err != nil {
		t.Fatalf("StartPrepare() error = %v", err)
	}
	if job.State != UIPreparationJobQueued || job.SelectedUI != installstate.UIEle {
		t.Fatalf("StartPrepare() = %#v, want queued workspace revalidation", job)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("workspace revalidation did not invoke the runner")
	}
	runner.release(nil)
}

func TestUIPreparationJobServiceRunsConfirmedResetAndSurfacesStableFailure(t *testing.T) {
	invalidService := NewUIPreparationJobService(newControllableUIPreparationRunner(), profileProviderStub{}, markerReaderStub{})
	t.Cleanup(func() { _ = invalidService.Close() })
	if _, err := invalidService.StartReset(context.Background(), UIResetRequest{}); !errors.Is(err, ErrUIPreparationInvalid) {
		t.Fatalf("unconfirmed StartReset() error = %v, want ErrUIPreparationInvalid", err)
	}
	if _, err := invalidService.StartReset(context.Background(), UIResetRequest{ConfirmReset: true}); !errors.Is(err, ErrUIPreparationConflict) {
		t.Fatalf("pristine StartReset() error = %v, want ErrUIPreparationConflict", err)
	}

	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(
		runner,
		profileProviderStub{exists: true, profile: InstallationProfile{SelectedUI: installstate.UIEle}},
		markerReaderStub{},
	)
	t.Cleanup(func() { _ = service.Close() })
	job, err := service.StartReset(context.Background(), UIResetRequest{ConfirmReset: true})
	if err != nil {
		t.Fatalf("StartReset() error = %v", err)
	}
	if job.Action != UIPreparationActionReset || job.State != UIPreparationJobQueued || job.SelectedUI != "" {
		t.Fatalf("StartReset() = %#v", job)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("reset runner did not start")
	}
	runner.release(errors.New("private command output fixture"))
	got := eventuallyUIPreparationJob(t, service, job.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobFailed
	})
	if got.CurrentStep != "failed" || got.ErrorKey != "ui_reset_failed" || got.Progress == 100 || got.LogPath != "" {
		t.Fatalf("failed reset Progress() = %#v", got)
	}
}

func TestUIPreparationJobServicePreservesStructuredFailureWithoutLeakingIrrelevantLog(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(runner, profileProviderStub{}, markerReaderStub{})
	t.Cleanup(func() { _ = service.Close() })

	job, err := service.StartPrepare(context.Background(), UIPrepareRequest{
		SelectedUI:     installstate.UIEle,
		ConfirmCleanup: true,
	})
	if err != nil {
		t.Fatalf("StartPrepare() error = %v", err)
	}
	<-runner.started
	runner.release(&UIPreparationFailure{
		ErrorKey:      "ui_preflight_failed",
		Step:          "preflight",
		Reason:        "preflight_failed",
		Scope:         "admin_apps",
		Operation:     "cross_directory_rename",
		DependencyLog: false,
	})

	got := eventuallyUIPreparationJob(t, service, job.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobFailed
	})
	if got.ErrorKey != "ui_preflight_failed" || got.FailureStep != "preflight" ||
		got.FailureReason != "preflight_failed" || got.FailureScope != "admin_apps" ||
		got.FailureOperation != "cross_directory_rename" {
		t.Fatalf("failed Progress() = %#v, want structured preflight diagnostic", got)
	}
	if got.LogPath != "" {
		t.Fatalf("preflight LogPath = %q, want no unrelated dependency log", got.LogPath)
	}
}

func TestUIPreparationJobServiceExposesDependencyLogOnlyForDependencyFailure(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(runner, profileProviderStub{}, markerReaderStub{})
	t.Cleanup(func() { _ = service.Close() })

	job, err := service.StartPrepare(context.Background(), UIPrepareRequest{
		SelectedUI:     installstate.UIAntd,
		ConfirmCleanup: true,
	})
	if err != nil {
		t.Fatalf("StartPrepare() error = %v", err)
	}
	<-runner.started
	runner.release(&UIPreparationFailure{
		ErrorKey:      "ui_dependency_install_failed",
		Step:          "dependencies",
		Reason:        "dependency_install_failed",
		DependencyLog: true,
	})

	got := eventuallyUIPreparationJob(t, service, job.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobFailed
	})
	if got.ErrorKey != "ui_dependency_install_failed" || got.FailureStep != "dependencies" ||
		got.LogPath != runner.logPath {
		t.Fatalf("dependency failure Progress() = %#v", got)
	}
}

func TestUIPreparationJobServiceRejectsUnlistedFailureFieldsAndRunnerLogPath(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	runner.logPath = "/private/secret/install.log"
	service := NewUIPreparationJobService(runner, profileProviderStub{}, markerReaderStub{})
	t.Cleanup(func() { _ = service.Close() })

	job, err := service.StartPrepare(context.Background(), UIPrepareRequest{
		SelectedUI:     installstate.UIAntd,
		ConfirmCleanup: true,
	})
	if err != nil {
		t.Fatalf("StartPrepare() error = %v", err)
	}
	<-runner.started
	runner.release(&UIPreparationFailure{
		ErrorKey:      "private_error_key",
		Step:          "dependencies",
		Reason:        "encoded_private_value",
		Scope:         "private_scope",
		Operation:     "private_operation",
		DependencyLog: true,
	})

	got := eventuallyUIPreparationJob(t, service, job.ID, func(got UIPreparationJob) bool {
		return got.State == UIPreparationJobFailed
	})
	if got.ErrorKey != "ui_prepare_failed" || got.FailureStep != "dependencies" ||
		got.FailureReason != "process_failed" || got.FailureScope != "" ||
		got.FailureOperation != "" || got.LogPath != "" {
		t.Fatalf("unlisted diagnostic crossed application boundary: %#v", got)
	}
}

func TestUIPreparationFailureAllowsInterruptedStateAndOperationReasons(t *testing.T) {
	fallback := UIPreparationFailure{
		ErrorKey: "ui_prepare_failed",
		Step:     "launch",
		Reason:   "process_failed",
	}
	for _, reason := range []string{"reset_in_progress", "state_inconsistent", "initialization_in_progress", "initialization_operation_failed"} {
		got := normalizedUIPreparationFailure(UIPreparationFailure{
			ErrorKey: "ui_prepare_failed",
			Step:     "preflight",
			Reason:   reason,
		}, fallback)
		if got.Reason != reason {
			t.Fatalf("normalized reason = %q, want %q", got.Reason, reason)
		}
	}
}

func TestUIPreparationJobServiceCloseCancelsServiceContextAndRejectsNewWork(t *testing.T) {
	runner := newControllableUIPreparationRunner()
	service := NewUIPreparationJobService(runner, profileProviderStub{}, markerReaderStub{})
	job, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIEle, ConfirmCleanup: true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("prepare runner did not start")
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := service.StartPrepare(context.Background(), UIPrepareRequest{SelectedUI: installstate.UIEle, ConfirmCleanup: true}); !errors.Is(err, ErrUIPreparationServiceClosed) {
		t.Fatalf("StartPrepare() after Close() error = %v, want ErrUIPreparationServiceClosed", err)
	}
	got, err := service.Progress(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Progress() after Close() error = %v", err)
	}
	if got.State != UIPreparationJobFailed || got.ErrorKey != "ui_prepare_failed" {
		t.Fatalf("Progress() after Close() = %#v", got)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestUIPreparationJobServiceProgressRejectsUnknownJobAndCancelledContext(t *testing.T) {
	service := NewUIPreparationJobService(newControllableUIPreparationRunner(), profileProviderStub{}, markerReaderStub{})
	t.Cleanup(func() { _ = service.Close() })
	if _, err := service.Progress(context.Background(), "missing"); !errors.Is(err, ErrUIPreparationJobNotFound) {
		t.Fatalf("Progress(missing) error = %v, want ErrUIPreparationJobNotFound", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Progress(ctx, "missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Progress(cancelled) error = %v, want context.Canceled", err)
	}
}

func eventuallyUIPreparationJob(t *testing.T, service *UIPreparationJobService, id string, predicate func(UIPreparationJob) bool) UIPreparationJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := service.Progress(context.Background(), id)
		if err != nil {
			t.Fatalf("Progress(%q) error = %v", id, err)
		}
		if predicate(job) {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	job, err := service.Progress(context.Background(), id)
	t.Fatalf("Progress(%q) timed out: job=%#v error=%v", id, job, err)
	return UIPreparationJob{}
}

type controllableUIPreparationRunner struct {
	started   chan struct{}
	releaseCh chan error
	logPath   string

	mu       sync.Mutex
	callback func(UIPreparationProgress)
	ui       installstate.UI
	prepares int
	resets   int
}

type mutableProfileProvider struct {
	mu      sync.Mutex
	profile InstallationProfile
	exists  bool
}

func (p *mutableProfileProvider) Profile(context.Context) (InstallationProfile, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.profile, p.exists, nil
}

func (p *mutableProfileProvider) set(profile InstallationProfile, exists bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.profile = profile
	p.exists = exists
}

type mutableMarkerReader struct {
	mu        sync.Mutex
	marker    installstate.Marker
	installed bool
}

func (r *mutableMarkerReader) Load(context.Context) (installstate.Marker, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.marker, r.installed, nil
}

func (r *mutableMarkerReader) set(marker installstate.Marker, installed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marker = marker
	r.installed = installed
}

func newControllableUIPreparationRunner() *controllableUIPreparationRunner {
	return &controllableUIPreparationRunner{
		started:   make(chan struct{}, 1),
		releaseCh: make(chan error, 1),
		logPath:   ".runtime/install/dependency-install.log",
	}
}

func (r *controllableUIPreparationRunner) Prepare(ctx context.Context, ui installstate.UI, report func(UIPreparationProgress)) error {
	r.mu.Lock()
	r.prepares++
	r.ui = ui
	r.callback = report
	r.mu.Unlock()
	r.started <- struct{}{}
	select {
	case err := <-r.releaseCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *controllableUIPreparationRunner) Reset(ctx context.Context, report func(UIPreparationProgress)) error {
	r.mu.Lock()
	r.resets++
	r.callback = report
	r.mu.Unlock()
	r.started <- struct{}{}
	select {
	case err := <-r.releaseCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *controllableUIPreparationRunner) LogPath() string { return r.logPath }

func (r *controllableUIPreparationRunner) report(progress UIPreparationProgress) {
	r.mu.Lock()
	callback := r.callback
	r.mu.Unlock()
	callback(progress)
}

func (r *controllableUIPreparationRunner) release(err error) { r.releaseCh <- err }

func (r *controllableUIPreparationRunner) prepareCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prepares
}

func (r *controllableUIPreparationRunner) prepareUI() installstate.UI {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ui
}
