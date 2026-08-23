package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	appjobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/jobs"
)

func TestRedisQueueRequiresConfiguredCache(t *testing.T) {
	queue := NewRedisQueue(nil, 0)
	if _, err := queue.Enqueue(context.Background(), appjobs.Task{Type: "manual", PayloadVersion: 1, IdempotencyKey: "key"}); !errors.Is(err, ErrRedisQueueUnavailable) {
		t.Fatalf("enqueue error = %v", err)
	}
	if _, err := queue.Get(context.Background(), "id"); !errors.Is(err, ErrRedisQueueUnavailable) {
		t.Fatalf("get error = %v", err)
	}
	if got := NewAsynqQueue(nil, 0); got == nil || got.maxAttempts != 3 {
		t.Fatalf("asynq seam = %+v", got)
	}
}

func TestRedisQueueStableErrorCodeDoesNotExposeProviderMessage(t *testing.T) {
	if got := stableErrorCode(errors.New("provider secret")); got != "worker.failed" {
		t.Fatalf("stable error code = %q", got)
	}
}

func TestTaskWireRoundTripsPayloadWithoutChangingQueueContract(t *testing.T) {
	original := appjobs.Task{ID: "queue-1", Type: "manual", PayloadVersion: 1, IdempotencyKey: "key", Payload: []byte(`{"value":1}`)}
	payload, err := json.Marshal(taskWire{Task: original, Payload: original.Payload})
	if err != nil {
		t.Fatal(err)
	}
	var decoded taskWire
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Task.ID != original.ID || string(decoded.Payload) != string(original.Payload) {
		t.Fatalf("decoded=%+v payload=%s", decoded.Task, decoded.Payload)
	}
}
