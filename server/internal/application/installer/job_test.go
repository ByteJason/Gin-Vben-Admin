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
	if _, err := service.Start(context.Background(), validApplyRequest()); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	<-started
	if _, err := service.Start(context.Background(), validApplyRequest()); !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("second Start() error = %v, want ErrApplyBusy", err)
	}
	close(release)
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

func (s *jobRunnerStub) ApplyWithProgress(ctx context.Context, request ApplyRequest, report func(string)) (ApplyResult, error) {
	s.mu.Lock()
	run := s.run
	s.mu.Unlock()
	return run(ctx, request, report)
}
