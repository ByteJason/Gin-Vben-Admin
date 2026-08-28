package installer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

var (
	ErrUIPreparationInvalid       = errors.New("UI preparation request is invalid")
	ErrUIPreparationConflict      = errors.New("UI preparation conflicts with the current installation state")
	ErrUIPreparationInstalled     = errors.New("installation is already complete")
	ErrUIPreparationJobNotFound   = errors.New("UI preparation job not found")
	ErrUIPreparationServiceClosed = errors.New("UI preparation job service is closed")
)

type UIPreparationAction string

const (
	UIPreparationActionPrepare UIPreparationAction = "prepare"
	UIPreparationActionReset   UIPreparationAction = "reset"
)

type UIPreparationJobState string

const (
	UIPreparationJobQueued    UIPreparationJobState = "queued"
	UIPreparationJobRunning   UIPreparationJobState = "running"
	UIPreparationJobSucceeded UIPreparationJobState = "succeeded"
	UIPreparationJobFailed    UIPreparationJobState = "failed"
)

type UIPrepareRequest struct {
	SelectedUI     installstate.UI `json:"selectedUi"`
	ConfirmCleanup bool            `json:"confirmCleanup"`
}

type UIResetRequest struct {
	ConfirmReset bool `json:"confirmReset"`
}

// UIPreparationJob is a bounded, credential-free view of one local UI
// initializer execution. Free-form command output is never copied into the
// public snapshot; LogPath is present only for a dependency-stage failure.
type UIPreparationJob struct {
	ID               string                `json:"id"`
	Action           UIPreparationAction   `json:"action"`
	State            UIPreparationJobState `json:"state"`
	SelectedUI       installstate.UI       `json:"selectedUi,omitempty"`
	CurrentStep      string                `json:"currentStep"`
	Progress         int                   `json:"progress"`
	ErrorKey         string                `json:"errorKey,omitempty"`
	FailureStep      string                `json:"failureStep,omitempty"`
	FailureReason    string                `json:"failureReason,omitempty"`
	FailureScope     string                `json:"failureScope,omitempty"`
	FailureOperation string                `json:"failureOperation,omitempty"`
	LogPath          string                `json:"logPath,omitempty"`
	LastUpdated      time.Time             `json:"lastUpdated"`
}

// UIPreparationFailure is the credential-free diagnostic contract returned by
// a platform runner. Every field is a stable identifier; free-form child
// output, command lines and absolute paths must never cross this boundary.
type UIPreparationFailure struct {
	ErrorKey      string
	Step          string
	Reason        string
	Scope         string
	Operation     string
	DependencyLog bool
}

const uiPreparationDependencyLogPath = ".runtime/install/dependency-install.log"

func (f *UIPreparationFailure) Error() string {
	if f == nil || !allowedUIPreparationErrorKey(f.ErrorKey) {
		return "UI preparation failed"
	}
	return f.ErrorKey
}

// UIPreparationProgress is the only process output allowed across the
// platform/application boundary. Implementations must report stable stages,
// never free-form command output.
type UIPreparationProgress struct {
	CurrentStep string
	Progress    int
}

type UIPreparationRunner interface {
	Prepare(context.Context, installstate.UI, func(UIPreparationProgress)) error
	Reset(context.Context, func(UIPreparationProgress)) error
	LogPath() string
}

// UIPreparationJobService owns a process-lifetime context. Request
// cancellation stops admission only; accepted local preparation continues
// until it finishes or the service is closed.
type UIPreparationJobService struct {
	runner   UIPreparationRunner
	profiles ProfileProvider
	markers  MarkerReader
	now      func() time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	mu     sync.Mutex
	jobs   map[string]UIPreparationJob
	active string
	closed bool
}

func NewUIPreparationJobService(runner UIPreparationRunner, profiles ProfileProvider, markers MarkerReader) *UIPreparationJobService {
	ctx, cancel := context.WithCancel(context.Background())
	return &UIPreparationJobService{
		runner: runner, profiles: profiles, markers: markers,
		now: time.Now, ctx: ctx, cancel: cancel,
		jobs: make(map[string]UIPreparationJob),
	}
}

func (s *UIPreparationJobService) StartPrepare(ctx context.Context, request UIPrepareRequest) (UIPreparationJob, error) {
	if !request.ConfirmCleanup || !validSelectedUI(request.SelectedUI) {
		return UIPreparationJob{}, ErrUIPreparationInvalid
	}
	if err := validateUIPreparationService(s, ctx); err != nil {
		return UIPreparationJob{}, err
	}

	_, installed, err := s.markers.Load(normalizeContext(ctx))
	if err != nil {
		return UIPreparationJob{}, fmt.Errorf("read installation marker: %w", err)
	}
	if installed {
		return UIPreparationJob{}, ErrUIPreparationInstalled
	}
	profile, exists, err := s.profiles.Profile(normalizeContext(ctx))
	if err != nil {
		return UIPreparationJob{}, fmt.Errorf("read installation profile: %w", err)
	}
	if exists {
		if !validProfile(profile) || profile.SelectedUI != request.SelectedUI {
			return UIPreparationJob{}, ErrUIPreparationConflict
		}
		if !profile.Installing {
			if !profile.IndependentUISelection {
				return s.completedPrepare(request.SelectedUI)
			}
			return s.enqueue(UIPreparationActionPrepare, request.SelectedUI)
		}
		if !profile.PreparingUI || profile.UIAction != UIPreparationActionPrepare {
			return UIPreparationJob{}, ErrUIPreparationConflict
		}
	}
	return s.enqueue(UIPreparationActionPrepare, request.SelectedUI)
}

func (s *UIPreparationJobService) StartReset(ctx context.Context, request UIResetRequest) (UIPreparationJob, error) {
	if !request.ConfirmReset {
		return UIPreparationJob{}, ErrUIPreparationInvalid
	}
	if err := validateUIPreparationService(s, ctx); err != nil {
		return UIPreparationJob{}, err
	}
	_, installed, err := s.markers.Load(normalizeContext(ctx))
	if err != nil {
		return UIPreparationJob{}, fmt.Errorf("read installation marker: %w", err)
	}
	if installed {
		return UIPreparationJob{}, ErrUIPreparationInstalled
	}
	if job, found, err := s.currentReset(); found || err != nil {
		return job, err
	}
	profile, exists, err := s.profiles.Profile(normalizeContext(ctx))
	if err != nil {
		return UIPreparationJob{}, fmt.Errorf("read installation profile: %w", err)
	}
	if !exists || !validProfile(profile) || profile.Installing && !profile.PreparingUI {
		return UIPreparationJob{}, ErrUIPreparationConflict
	}
	return s.enqueue(UIPreparationActionReset, "")
}

func (s *UIPreparationJobService) Progress(ctx context.Context, id string) (UIPreparationJob, error) {
	if s == nil {
		return UIPreparationJob{}, ErrUIPreparationJobNotFound
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return UIPreparationJob{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return UIPreparationJob{}, ErrUIPreparationJobNotFound
	}
	return job, nil
}

func (s *UIPreparationJobService) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.cancel != nil {
			s.cancel()
		}
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("UI preparation jobs did not stop")
	}
}

func validateUIPreparationService(s *UIPreparationJobService, ctx context.Context) error {
	if s == nil || s.runner == nil || s.profiles == nil || s.markers == nil {
		return errors.New("UI preparation job service is not configured")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUIPreparationServiceClosed
	}
	return nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func validSelectedUI(ui installstate.UI) bool {
	switch ui {
	case installstate.UIAntd, installstate.UIEle, installstate.UINaive:
		return true
	default:
		return false
	}
}

func (s *UIPreparationJobService) currentReset() (UIPreparationJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return UIPreparationJob{}, false, ErrUIPreparationServiceClosed
	}
	if active, ok := s.jobs[s.active]; ok && (active.State == UIPreparationJobQueued || active.State == UIPreparationJobRunning) {
		if active.Action == UIPreparationActionReset {
			return active, true, nil
		}
		return UIPreparationJob{}, false, ErrUIPreparationConflict
	}
	return UIPreparationJob{}, false, nil
}

func (s *UIPreparationJobService) completedPrepare(ui installstate.UI) (UIPreparationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return UIPreparationJob{}, ErrUIPreparationServiceClosed
	}
	if active, ok := s.jobs[s.active]; ok && (active.State == UIPreparationJobQueued || active.State == UIPreparationJobRunning) {
		if active.Action == UIPreparationActionPrepare && active.SelectedUI == ui {
			return active, nil
		}
		return UIPreparationJob{}, ErrUIPreparationConflict
	}
	var latest UIPreparationJob
	for _, job := range s.jobs {
		if job.Action == UIPreparationActionPrepare && job.SelectedUI == ui && job.State == UIPreparationJobSucceeded &&
			(latest.ID == "" || job.LastUpdated.After(latest.LastUpdated)) {
			latest = job
		}
	}
	if latest.ID != "" {
		return latest, nil
	}
	id, err := newUIPreparationJobID()
	if err != nil {
		return UIPreparationJob{}, errors.New("create UI preparation job")
	}
	now := s.now().UTC()
	job := UIPreparationJob{
		ID: id, Action: UIPreparationActionPrepare, State: UIPreparationJobSucceeded,
		SelectedUI: ui, CurrentStep: "complete", Progress: 100,
		LastUpdated: now,
	}
	s.jobs[id] = job
	s.pruneLocked(id)
	return job, nil
}

func (s *UIPreparationJobService) enqueue(action UIPreparationAction, ui installstate.UI) (UIPreparationJob, error) {
	id, err := newUIPreparationJobID()
	if err != nil {
		return UIPreparationJob{}, errors.New("create UI preparation job")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return UIPreparationJob{}, ErrUIPreparationServiceClosed
	}
	if active, ok := s.jobs[s.active]; ok && (active.State == UIPreparationJobQueued || active.State == UIPreparationJobRunning) {
		if active.Action == action && (action == UIPreparationActionReset || active.SelectedUI == ui) {
			s.mu.Unlock()
			return active, nil
		}
		s.mu.Unlock()
		return UIPreparationJob{}, ErrUIPreparationConflict
	}
	now := s.now().UTC()
	job := UIPreparationJob{
		ID: id, Action: action, State: UIPreparationJobQueued, SelectedUI: ui,
		CurrentStep: "queued", Progress: 0, LastUpdated: now,
	}
	s.jobs[id] = job
	s.active = id
	s.pruneLocked(id)
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		s.run(id, action, ui)
	}()
	return job, nil
}

func (s *UIPreparationJobService) run(id string, action UIPreparationAction, ui installstate.UI) {
	s.updateState(id, UIPreparationJobRunning, "launch", 5, "")
	report := func(progress UIPreparationProgress) {
		if !validUIPreparationStep(progress.CurrentStep) || progress.Progress < 0 || progress.Progress > 99 {
			return
		}
		s.updateState(id, UIPreparationJobRunning, progress.CurrentStep, progress.Progress, "")
	}
	var err error
	if action == UIPreparationActionReset {
		err = s.runner.Reset(s.ctx, report)
	} else {
		err = s.runner.Prepare(s.ctx, ui, report)
	}
	if err != nil {
		errorKey := "ui_prepare_failed"
		if action == UIPreparationActionReset {
			errorKey = "ui_reset_failed"
		}
		failure := UIPreparationFailure{
			ErrorKey: errorKey,
			Step:     "launch",
			Reason:   "process_failed",
		}
		var structured *UIPreparationFailure
		if errors.As(err, &structured) {
			failure = normalizedUIPreparationFailure(*structured, failure)
		}
		s.updateFailure(id, failure)
		return
	}
	s.updateState(id, UIPreparationJobSucceeded, "complete", 100, "")
}

func validUIPreparationStep(step string) bool {
	switch step {
	case "queued", "launch", "preflight", "workspace", "dependencies", "reset", "complete", "failed":
		return true
	default:
		return false
	}
}

func normalizedUIPreparationFailure(candidate, fallback UIPreparationFailure) UIPreparationFailure {
	trustedError := allowedUIPreparationErrorKey(candidate.ErrorKey)
	trustedReason := allowedUIPreparationFailureReason(candidate.Reason)
	if trustedError {
		fallback.ErrorKey = candidate.ErrorKey
	}
	if validUIPreparationStep(candidate.Step) && candidate.Step != "queued" && candidate.Step != "complete" && candidate.Step != "failed" {
		fallback.Step = candidate.Step
	}
	if trustedReason {
		fallback.Reason = candidate.Reason
	}
	if allowedUIPreparationFailureScope(candidate.Scope) {
		fallback.Scope = candidate.Scope
	}
	if allowedUIPreparationFailureOperation(candidate.Operation) {
		fallback.Operation = candidate.Operation
	}
	fallback.DependencyLog = candidate.DependencyLog && fallback.Step == "dependencies" && trustedError && trustedReason
	return fallback
}

func allowedUIPreparationErrorKey(value string) bool {
	switch value {
	case "ui_prepare_failed", "ui_reset_failed", "ui_preflight_failed", "ui_template_layout_invalid",
		"ui_api_unavailable", "ui_initialization_busy", "ui_initialization_lease_failed",
		"ui_state_directory_invalid", "ui_node_version_unsupported", "ui_pnpm_version_unsupported",
		"ui_dependency_install_failed", "ui_workspace_prepare_failed", "ui_switch_failed",
		"ui_workspace_layout_invalid", "ui_workspace_transaction_invalid":
		return true
	default:
		return false
	}
}

func allowedUIPreparationFailureReason(value string) bool {
	switch value {
	case "process_failed", "preflight_failed", "template_layout_invalid", "api_unavailable",
		"init_busy", "init_lease_failed", "install_state_dir_invalid", "node_version_unsupported",
		"pnpm_version_unsupported", "dependency_install_failed", "dependency_transaction_invalid",
		"source_move_state_invalid", "initialization_resume_invalid", "dependency_install_busy",
		"reset_layout_invalid", "reset_receipt_unavailable", "reset_transaction_invalid",
		"reset_unavailable", "reset_unavailable_installed", "legacy_migration_invalid",
		"recovery_validation_failed", "runtime_env_app_invalid", "runtime_env_profile_invalid",
		"runtime_env_target_invalid", "runtime_env_template_invalid", "ui_invalid",
		"ui_package_mismatch", "ui_profile_invalid", "ui_profile_mismatch", "ui_profile_required",
		"reset_in_progress", "state_inconsistent", "initialization_in_progress", "initialization_operation_failed",
		"workspace_layout_invalid", "workspace_transaction_invalid", "ui_switch_failed":
		return true
	default:
		return false
	}
}

func allowedUIPreparationFailureScope(value string) bool {
	switch value {
	case "admin_root", "admin_apps", "selected_ui", "state_root", "ui_backup":
		return true
	default:
		return false
	}
}

func allowedUIPreparationFailureOperation(value string) bool {
	switch value {
	case "read", "create", "write", "sync", "link", "rename", "delete", "cross_directory_rename", "execute", "lock":
		return true
	default:
		return false
	}
}

func (s *UIPreparationJobService) updateFailure(id string, failure UIPreparationFailure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.State = UIPreparationJobFailed
	job.CurrentStep = "failed"
	job.ErrorKey = failure.ErrorKey
	job.FailureStep = failure.Step
	job.FailureReason = failure.Reason
	job.FailureScope = failure.Scope
	job.FailureOperation = failure.Operation
	if failure.DependencyLog && s.runner.LogPath() == uiPreparationDependencyLogPath {
		job.LogPath = uiPreparationDependencyLogPath
	} else {
		job.LogPath = ""
	}
	job.LastUpdated = s.now().UTC()
	s.jobs[id] = job
	if s.active == id {
		s.active = ""
	}
}

func (s *UIPreparationJobService) updateState(id string, state UIPreparationJobState, step string, progress int, errorKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.State = state
	job.CurrentStep = step
	if progress >= 0 {
		job.Progress = progress
	}
	job.ErrorKey = errorKey
	job.LastUpdated = s.now().UTC()
	s.jobs[id] = job
	if state == UIPreparationJobSucceeded || state == UIPreparationJobFailed {
		if s.active == id {
			s.active = ""
		}
	}
}

func (s *UIPreparationJobService) pruneLocked(keep string) {
	const maxJobs = 8
	for len(s.jobs) > maxJobs {
		oldestID := ""
		var oldest time.Time
		for id, job := range s.jobs {
			if id == keep || id == s.active || job.State == UIPreparationJobQueued || job.State == UIPreparationJobRunning {
				continue
			}
			if oldestID == "" || job.LastUpdated.Before(oldest) {
				oldestID, oldest = id, job.LastUpdated
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.jobs, oldestID)
	}
}

func newUIPreparationJobID() (string, error) {
	id, err := newJobID()
	if err != nil {
		return "", err
	}
	return "ui-prepare-" + id[len("install-"):], nil
}
