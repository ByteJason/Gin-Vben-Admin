package tasks

import (
	"context"
	"encoding/json"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/jobs"
	"time"
)

// ExecutionSpec is the internal versioned queue snapshot, not an HTTP input.
type ExecutionSpec struct {
	RunID          string          `json:"runId"`
	ExecutorType   string          `json:"executorType"`
	MethodKey      string          `json:"methodKey"`
	HTTP           json.RawMessage `json:"http"`
	Payload        json.RawMessage `json:"payload"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
}
type HTTPTaskExecutor interface {
	Execute(context.Context, json.RawMessage) (ExecutionResult, error)
}

func (s *RunService) BindExecutors(worker *jobs.Worker, methods *MethodExecutor, http HTTPTaskExecutor) error {
	handler := func(ctx context.Context, queued jobs.Task) error {
		var spec ExecutionSpec
		if json.Unmarshal(queued.Payload, &spec) != nil || spec.RunID == "" {
			return ErrInvalidRunPayload
		}
		seconds := spec.TimeoutSeconds
		if seconds < 1 {
			seconds = 30
		}
		if seconds > 3600 {
			seconds = 3600
		}
		callCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
		var result ExecutionResult
		var err error
		if spec.ExecutorType == "http" {
			if http == nil {
				return ErrExecutorNotRegistered
			}
			var config map[string]any
			if json.Unmarshal(spec.HTTP, &config) != nil {
				return ErrInvalidHTTPPayload
			}
			config["timeoutSeconds"] = seconds
			raw, _ := json.Marshal(config)
			result, err = http.Execute(callCtx, raw)
		} else {
			result, err = methods.ExecuteByKey(callCtx, spec.MethodKey, spec.Payload)
		}
		if callCtx.Err() != nil && err == nil {
			err = callCtx.Err()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, saveErr := s.SetExecutionResult(context.WithoutCancel(ctx), spec.RunID, result); saveErr != nil {
			return saveErr
		}
		return err
	}
	for _, kind := range []string{"manual", "http", "webhook"} {
		if err := s.BindWorker(worker, kind, handler); err != nil {
			return err
		}
	}
	return nil
}
