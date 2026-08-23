package installer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

var (
	ErrJobNotFound                  = errors.New("installation job not found")
	ErrJobServiceClosed             = errors.New("installation job service is closed")
	ErrRollbackUnavailable          = errors.New("installation rollback is unavailable")
	ErrRollbackConfirmationRequired = errors.New("installation rollback confirmation is required")
)

type JobState string

const (
	JobRunning   JobState = "running"
	JobCompleted JobState = "completed"
	JobFailed    JobState = "failed"
)

type ApplyJob struct {
	ID          string            `json:"id"`
	State       JobState          `json:"state"`
	SelectedUI  installstate.UI   `json:"selectedUi"`
	Mode        installstate.Mode `json:"mode"`
	CurrentStep string            `json:"currentStep"`
	Progress    int               `json:"progress"`
	Steps       []ApplyStep       `json:"steps"`
	InstalledAt *time.Time        `json:"installedAt,omitempty"`
	ErrorCode   int               `json:"errorCode,omitempty"`
	ErrorKey    string            `json:"errorKey,omitempty"`
	CanRetry    bool              `json:"canRetry"`
	CanRollback bool              `json:"canRollback"`
	LastUpdated time.Time         `json:"lastUpdated"`
}

// RollbackResult is the credential-free result of an explicit recovery
// request for a failed installation transaction. It deliberately does not
// expose a database DSN, file path, or transaction receipt.
type RollbackResult struct {
	JobID       string    `json:"jobId"`
	State       JobState  `json:"state"`
	CurrentStep string    `json:"currentStep"`
	RolledBack  bool      `json:"rolledBack"`
	CanRetry    bool      `json:"canRetry"`
	LastUpdated time.Time `json:"lastUpdated"`
}

type RollbackRequest struct {
	ConfirmRollback bool `json:"confirmRollback"`
}

type ProgressApplyRunner interface {
	ApplyWithProgress(context.Context, ApplyRequest, func(string)) (ApplyResult, error)
}

type RollbackRunner interface {
	Rollback(context.Context) error
}

type RollbackAvailability interface {
	CanRollback() bool
}

// ApplyJobService keeps credential-bearing requests only in the active
// goroutine while publishing a bounded, credential-free progress snapshot.
type ApplyJobService struct {
	runner ProgressApplyRunner
	now    func() time.Time
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu               sync.Mutex
	jobs             map[string]ApplyJob
	active           string
	closed           bool
	rollbackInFlight bool
}

func NewApplyJobService(runner ProgressApplyRunner) *ApplyJobService {
	ctx, cancel := context.WithCancel(context.Background())
	return &ApplyJobService{runner: runner, now: time.Now, ctx: ctx, cancel: cancel, jobs: make(map[string]ApplyJob)}
}

func (s *ApplyJobService) Start(ctx context.Context, request ApplyRequest) (ApplyJob, error) {
	if s == nil || s.runner == nil {
		return ApplyJob{}, errors.New("installation job service is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ApplyJob{}, err
	}
	ui, mode, err := validateApplyRequest(request)
	if err != nil {
		return ApplyJob{}, err
	}
	id, err := newJobID()
	if err != nil {
		return ApplyJob{}, errors.New("create installation job")
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ApplyJob{}, ErrJobServiceClosed
	}
	if active, ok := s.jobs[s.active]; ok && active.State == JobRunning {
		s.mu.Unlock()
		return ApplyJob{}, ErrApplyBusy
	}
	if s.rollbackInFlight {
		s.mu.Unlock()
		return ApplyJob{}, ErrApplyBusy
	}
	now := s.now().UTC()
	job := ApplyJob{
		ID: id, State: JobRunning, SelectedUI: ui, Mode: mode,
		CurrentStep: "queued", Progress: 0, LastUpdated: now,
	}
	s.jobs[id] = job
	s.active = id
	s.pruneLocked(id)
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		s.run(id, request)
	}()
	return cloneApplyJob(job), nil
}

func (s *ApplyJobService) Progress(ctx context.Context, id string) (ApplyJob, error) {
	if s == nil {
		return ApplyJob{}, ErrJobNotFound
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return ApplyJob{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return ApplyJob{}, ErrJobNotFound
	}
	return cloneApplyJob(job), nil
}

func (s *ApplyJobService) Retry(ctx context.Context, id string, request ApplyRequest) (ApplyJob, error) {
	if s == nil {
		return ApplyJob{}, ErrJobNotFound
	}
	s.mu.Lock()
	job, ok := s.jobs[id]
	canRetry := ok && job.State == JobFailed && job.CanRetry
	s.mu.Unlock()
	if !ok {
		return ApplyJob{}, ErrJobNotFound
	}
	if !canRetry {
		return ApplyJob{}, ErrInvalidApply
	}
	return s.Start(ctx, request)
}

func (s *ApplyJobService) Close() error {
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
		return errors.New("installation jobs did not stop")
	}
}

func (s *ApplyJobService) run(id string, request ApplyRequest) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	defer cancel()
	result, err := s.runner.ApplyWithProgress(ctx, request, func(step string) {
		s.updateProgress(id, step)
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	job.LastUpdated = s.now().UTC()
	if err != nil {
		job.State = JobFailed
		job.ErrorCode, job.ErrorKey, job.CanRetry = publicJobError(err)
		job.CanRollback = false
		if _, ok := s.runner.(RollbackRunner); ok && errors.Is(err, ErrApplyRollback) {
			job.CanRollback = true
			if availability, ok := s.runner.(RollbackAvailability); ok {
				job.CanRollback = availability.CanRollback()
			}
		}
		if s.closed {
			job.CanRetry = false
		}
		job.CurrentStep = "failed"
		s.jobs[id] = job
		if s.active == id {
			s.active = ""
		}
		return
	}
	job.State = JobCompleted
	job.SelectedUI = result.SelectedUI
	job.Mode = result.Mode
	job.CurrentStep = "complete"
	job.Progress = 100
	job.Steps = append([]ApplyStep(nil), result.Steps...)
	if !result.InstalledAt.IsZero() {
		installedAt := result.InstalledAt
		job.InstalledAt = &installedAt
	}
	job.ErrorCode = 0
	job.ErrorKey = ""
	job.CanRetry = false
	job.CanRollback = false
	s.jobs[id] = job
	if s.active == id {
		s.active = ""
	}
}

// Rollback explicitly compensates a failed job. The caller must provide an
// affirmative confirmation; no credentials or infrastructure details are
// accepted by this operation.
func (s *ApplyJobService) Rollback(ctx context.Context, id string, confirmed bool) (RollbackResult, error) {
	if s == nil {
		return RollbackResult{}, ErrJobNotFound
	}
	if !confirmed {
		return RollbackResult{}, ErrRollbackConfirmationRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RollbackResult{}, err
	}
	runner, ok := s.runner.(RollbackRunner)
	if !ok {
		return RollbackResult{}, ErrRollbackUnavailable
	}
	s.mu.Lock()
	job, exists := s.jobs[id]
	if !exists {
		s.mu.Unlock()
		return RollbackResult{}, ErrJobNotFound
	}
	if job.State != JobFailed || !job.CanRollback {
		s.mu.Unlock()
		return RollbackResult{}, ErrRollbackUnavailable
	}
	if s.rollbackInFlight {
		s.mu.Unlock()
		return RollbackResult{}, ErrApplyBusy
	}
	s.rollbackInFlight = true
	s.mu.Unlock()

	err := runner.Rollback(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbackInFlight = false
	if err != nil {
		return RollbackResult{}, ErrRollbackUnavailable
	}
	job.CanRollback = false
	job.CanRetry = true
	job.CurrentStep = "rolled_back"
	job.LastUpdated = s.now().UTC()
	s.jobs[id] = job
	return RollbackResult{
		JobID: id, State: job.State, CurrentStep: job.CurrentStep,
		RolledBack: true, CanRetry: job.CanRetry, LastUpdated: job.LastUpdated,
	}, nil
}

func (s *ApplyJobService) updateProgress(id, step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok || job.State != JobRunning {
		return
	}
	job.CurrentStep = step
	job.Progress = stepProgress(step)
	job.Steps = appendCompletedOnce(job.Steps, step)
	job.LastUpdated = s.now().UTC()
	s.jobs[id] = job
}

func (s *ApplyJobService) pruneLocked(keep string) {
	const maxJobs = 8
	for len(s.jobs) > maxJobs {
		var oldestID string
		var oldest time.Time
		for id, job := range s.jobs {
			if id == keep || job.State == JobRunning {
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

func newJobID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "install-" + hex.EncodeToString(value[:]), nil
}

func stepProgress(step string) int {
	return map[string]int{
		"plan": 10, "database": 20, "redis": 30, "schema": 45,
		"assets": 65, "identity": 80, "environment": 90, "lock": 100,
	}[step]
}

func appendCompletedOnce(steps []ApplyStep, id string) []ApplyStep {
	for _, step := range steps {
		if step.ID == id {
			return steps
		}
	}
	return append(steps, ApplyStep{ID: id, Status: StepCompleted})
}

func publicJobError(err error) (int, string, bool) {
	switch {
	case errors.Is(err, ErrAlreadyInstalled):
		return 10006, "installation_completed", false
	case errors.Is(err, ErrApplyBusy):
		return 10007, "installation_running", true
	case errors.Is(err, ErrInvalidApply):
		return 10000, "invalid_request", false
	case errors.Is(err, ErrPreflightFailed):
		return 10001, "validation_failed", true
	default:
		return 50000, "internal_error", true
	}
}

func cloneApplyJob(job ApplyJob) ApplyJob {
	job.Steps = append([]ApplyStep(nil), job.Steps...)
	if job.InstalledAt != nil {
		installedAt := *job.InstalledAt
		job.InstalledAt = &installedAt
	}
	return job
}
