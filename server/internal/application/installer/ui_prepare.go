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
// initializer execution. Command output stays in LogPath and is never copied
// into the public snapshot.
type UIPreparationJob struct {
	ID          string                `json:"id"`
	Action      UIPreparationAction   `json:"action"`
	State       UIPreparationJobState `json:"state"`
	SelectedUI  installstate.UI       `json:"selectedUi,omitempty"`
	CurrentStep string                `json:"currentStep"`
	Progress    int                   `json:"progress"`
	ErrorKey    string                `json:"errorKey,omitempty"`
	LogPath     string                `json:"logPath,omitempty"`
	LastUpdated time.Time             `json:"lastUpdated"`
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
			return s.completedPrepare(request.SelectedUI)
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
		LogPath: s.runner.LogPath(), LastUpdated: now,
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
		CurrentStep: "queued", Progress: 0, LogPath: s.runner.LogPath(), LastUpdated: now,
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
	s.updateState(id, UIPreparationJobRunning, "preflight", 5, "")
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
		s.updateState(id, UIPreparationJobFailed, "failed", -1, errorKey)
		return
	}
	s.updateState(id, UIPreparationJobSucceeded, "complete", 100, "")
}

func validUIPreparationStep(step string) bool {
	switch step {
	case "queued", "preflight", "workspace", "dependencies", "reset", "complete", "failed":
		return true
	default:
		return false
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
