package installer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestApplyJobReportsProgressAndCompletes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runner := &jobRunnerStub{run: func(_ context.Context, _ ApplyRequest, report func(string)) (ApplyResult, error) {
		report("database")
		close(started)
		<-release
		report("lock")
		return ApplyResult{State: StateInstalled, SelectedUI: "antd", Mode: "embedded", Steps: []ApplyStep{{ID: "database", Status: StepCompleted}, {ID: "lock", Status: StepCompleted}}}, nil
	}}
	service := NewApplyJobService(runner)
	job, err := service.Start(context.Background(), validApplyRequest())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if job.State != JobRunning || job.Progress != 0 || job.ID == "" {
		t.Fatalf("initial job = %#v", job)
	}
	<-started
	progress, err := service.Progress(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Progress() error = %v", err)
	}
	if progress.CurrentStep != "database" || progress.Progress != 20 {
		t.Fatalf("running progress = %#v", progress)
	}
	close(release)
	completed := waitForJob(t, service, job.ID, JobCompleted)
	if completed.Progress != 100 || completed.SelectedUI != "antd" || len(completed.Steps) != 2 || completed.ErrorCode != 0 {
		t.Fatalf("completed job = %#v", completed)
	}
}

func TestApplyJobRejectsConcurrentStart(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runner := &jobRunnerStub{run: func(context.Context, ApplyRequest, func(string)) (ApplyResult, error) {
		close(started)
		<-release
		return ApplyResult{State: StateInstalled}, nil
	}}
	service := NewApplyJobService(runner)
	job, err := service.Start(context.Background(), validApplyRequest())
	if err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	<-started
	if !service.InstallationActive() {
		t.Fatal("InstallationActive() = false while runner is blocked")
	}
	if _, err := service.Start(context.Background(), validApplyRequest()); !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("second Start() error = %v, want ErrApplyBusy", err)
	}
	close(release)
	waitForJob(t, service, job.ID, JobCompleted)
	if service.InstallationActive() {
		t.Fatal("InstallationActive() = true after runner completed")
	}
}

func TestApplyJobRetryRequiresFailedJob(t *testing.T) {
	runner := &jobRunnerStub{run: func(context.Context, ApplyRequest, func(string)) (ApplyResult, error) {
		return ApplyResult{}, ErrApplyFailed
	}}
	service := NewApplyJobService(runner)
	job, err := service.Start(context.Background(), validApplyRequest())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForJob(t, service, job.ID, JobFailed)
	retry, err := service.Retry(context.Background(), job.ID, validApplyRequest())
	if err != nil {
		t.Fatalf("Retry() error = %v", err)
	}
	if retry.ID == job.ID || retry.State != JobRunning {
		t.Fatalf("retry job = %#v", retry)
	}
}

func TestApplyJobCloseCancelsActiveRunner(t *testing.T) {
	started := make(chan struct{})
	runner := &jobRunnerStub{run: func(ctx context.Context, _ ApplyRequest, _ func(string)) (ApplyResult, error) {
		close(started)
		<-ctx.Done()
		return ApplyResult{}, ctx.Err()
	}}
	service := NewApplyJobService(runner)
	job, err := service.Start(context.Background(), validApplyRequest())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-started
	if err := service.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	waitForJob(t, service, job.ID, JobFailed)
	if _, err := service.Start(context.Background(), validApplyRequest()); !errors.Is(err, ErrJobServiceClosed) {
		t.Fatalf("Start() after Close error = %v, want ErrJobServiceClosed", err)
	}
}

func TestApplyJobExplicitRollbackRequiresFailedRollbackableJob(t *testing.T) {
	runner := &rollbackJobRunnerStub{}
	service := NewApplyJobService(runner)
	job, err := service.Start(context.Background(), validApplyRequest())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	failed := waitForJob(t, service, job.ID, JobFailed)
	if !failed.CanRollback {
		t.Fatalf("failed job = %#v, want CanRollback", failed)
	}
	if _, err := service.Rollback(context.Background(), job.ID, false); !errors.Is(err, ErrRollbackConfirmationRequired) {
		t.Fatalf("unconfirmed Rollback() error = %v, want confirmation error", err)
	}
	result, err := service.Rollback(context.Background(), job.ID, true)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if !result.RolledBack || result.JobID != job.ID || result.CurrentStep != "rolled_back" || runner.rollbackCalls != 1 {
		t.Fatalf("rollback result = %#v; calls=%d", result, runner.rollbackCalls)
	}
	progress, err := service.Progress(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Progress() after rollback error = %v", err)
	}
	if progress.CanRollback {
		t.Fatalf("progress after rollback = %#v, want CanRollback=false", progress)
	}
}

func waitForJob(t *testing.T, service *ApplyJobService, id string, state JobState) ApplyJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := service.Progress(context.Background(), id)
		if err != nil {
			t.Fatalf("Progress() error = %v", err)
		}
		if job.State == state {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, state)
	return ApplyJob{}
}

type jobRunnerStub struct {
	mu  sync.Mutex
	run func(context.Context, ApplyRequest, func(string)) (ApplyResult, error)
}

type rollbackJobRunnerStub struct {
	rollbackCalls int
}

func (s *rollbackJobRunnerStub) ApplyWithProgress(context.Context, ApplyRequest, func(string)) (ApplyResult, error) {
	return ApplyResult{}, errors.Join(ErrApplyFailed, ErrApplyRollback)
}

func (s *rollbackJobRunnerStub) Rollback(context.Context) error {
	s.rollbackCalls++
	return nil
}

func (s *rollbackJobRunnerStub) CanRollback() bool {
	return true
}

func (s *jobRunnerStub) ApplyWithProgress(ctx context.Context, request ApplyRequest, report func(string)) (ApplyResult, error) {
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	return run(ctx, request, report)
}
